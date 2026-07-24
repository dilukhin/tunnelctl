package supervisor

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"tunnelctl/internal/config"
	"tunnelctl/internal/logx"
	"tunnelctl/internal/sshproxy"
)

type ProfileRunner interface {
	Run(context.Context, config.Config, config.Profile, bool, sshproxy.Observer) error
}

type defaultRunner struct{}

func (defaultRunner) Run(ctx context.Context, cfg config.Config, p config.Profile, watch bool, observe sshproxy.Observer) error {
	return sshproxy.RunOnceObserved(ctx, cfg, p, watch, observe)
}

type Options struct {
	Config     config.Config
	ConfigPath string
	Target     string
	Watch      bool
	Runner     ProfileRunner
}

// Run запускает управляемый супервизор туннеля.
func Run(ctx context.Context, opts Options) error {
	if opts.Runner == nil {
		opts.Runner = defaultRunner{}
	}
	if err := opts.Config.Validate(); err != nil {
		return err
	}
	opts.ConfigPath = config.EffectivePath(opts.ConfigPath)
	absoluteConfigPath, err := filepath.Abs(opts.ConfigPath)
	if err != nil {
		return err
	}
	opts.ConfigPath = absoluteConfigPath

	targetType, profiles, err := resolveTarget(opts.Config, opts.Target)
	if err != nil {
		return err
	}

	server, err := listenControl()
	if err != nil {
		return err
	}

	store := newStateStore(opts.ConfigPath, opts.Target, targetType)
	if err := store.update(func(s *State) { s.Status = "ожидание запуска" }); err != nil {
		_ = server.Close()
		store.remove()
		return err
	}

	runErr := runLoop(ctx, opts, profiles, targetType == "group", server, store)
	_ = server.Close()
	store.remove()

	var restartErr *restartRequestedError
	if errors.As(runErr, &restartErr) {
		return replaceCurrentProcess(restartErr.executable, opts)
	}
	return runErr
}

func resolveTarget(cfg config.Config, target string) (string, []config.Profile, error) {
	if p, ok := cfg.ResolveProfile(target); ok {
		return "profile", []config.Profile{p}, nil
	}
	g, ok := cfg.ResolveGroup(target)
	if !ok {
		return "", nil, fmt.Errorf("профиль или группа не найдены: %s", target)
	}
	if len(g.Profiles) == 0 {
		return "", nil, fmt.Errorf("группа %s пуста", target)
	}
	profiles := make([]config.Profile, 0, len(g.Profiles))
	for _, name := range g.Profiles {
		p, ok := cfg.ProfileByName(name)
		if !ok {
			return "", nil, fmt.Errorf("в группе %s указан неизвестный профиль: %s", target, name)
		}
		profiles = append(profiles, p)
	}
	return "group", profiles, nil
}

type pendingSwitch struct {
	target        config.Profile
	previous      config.Profile
	fallbackIndex int
	reply         func(Response)
}

func runLoop(ctx context.Context, opts Options, groupProfiles []config.Profile, isGroup bool, server controlServer, store *stateStore) error {
	minDelay, maxDelay := reconnectBounds(opts.Config)
	retryDelay := minDelay
	cycleDelay := minDelay
	groupIndex := 0
	current := groupProfiles[0]
	var pending *pendingSwitch
	manualActive := false

	for {
		if err := sshproxy.WaitPortFree(ctx, current.EffectiveListen(opts.Config), 5*time.Second); err != nil {
			if pending != nil {
				pending.reply(Response{OK: false, Error: fmt.Sprintf("переключение на профиль %s не выполнено: %v", current.Name, err)})
				current = pending.previous
				pending = nil
				continue
			}
			return err
		}

		attemptCtx, cancelAttempt := context.WithCancel(ctx)
		events := make(chan sshproxy.Event, 16)
		result := make(chan error, 1)
		go func(profile config.Profile) {
			result <- opts.Runner.Run(attemptCtx, opts.Config, profile, opts.Watch, func(event sshproxy.Event) {
				select {
				case events <- event:
				case <-attemptCtx.Done():
				}
			})
		}(current)

		switchRequested := false
		var stopReply func(Response)
		var restartReply func(Response)
		restartExecutable := ""
		healthy := false

	attempt:
		for {
			select {
			case <-ctx.Done():
				cancelAttempt()
				<-result
				_ = store.update(func(s *State) { s.Status = "остановлен" })
				return nil

			case event := <-events:
				switch event.Type {
				case sshproxy.EventStarting:
					_ = store.update(func(s *State) {
						s.ActiveProfile = event.Profile
						s.Listen = event.Listen
						s.Status = "запуск SSH"
						s.LastHealthError = ""
					})
				case sshproxy.EventListening:
					_ = store.update(func(s *State) {
						s.ActiveProfile = event.Profile
						s.Listen = event.Listen
						s.Status = "SOCKS-порт открыт, выполняется проверка"
					})
				case sshproxy.EventHealthSuccess:
					healthy = true
					retryDelay = minDelay
					cycleDelay = minDelay
					_ = store.update(func(s *State) {
						s.ActiveProfile = event.Profile
						s.Listen = event.Listen
						s.Status = "работает"
						s.LastHealthSuccess = event.Time
						s.LastHealthError = ""
					})
					if pending != nil && pending.target.Name == event.Profile {
						pending.reply(Response{OK: true, Message: fmt.Sprintf("Активный профиль: %s", event.Profile), State: statePtr(store.snapshot())})
						pending = nil
						manualActive = true
						if idx := profileIndex(groupProfiles, event.Profile); idx >= 0 {
							groupIndex = idx
						}
					}
				case sshproxy.EventHealthFailure:
					_ = store.update(func(s *State) {
						s.Status = "ошибка проверки"
						s.LastHealthError = safeError(event.Err)
					})
				}

			case env, ok := <-server.Requests():
				if !ok {
					cancelAttempt()
					<-result
					return errors.New("управляющий канал неожиданно остановлен")
				}
				if env.Request.Version != 0 && env.Request.Version != protocolVersion {
					env.Reply(Response{OK: false, Error: "несовместимая версия протокола управления"})
					continue
				}
				switch env.Request.Action {
				case "status":
					snapshot := store.snapshot()
					env.Reply(Response{OK: true, State: &snapshot})
				case "stop":
					if restartReply != nil {
						env.Reply(Response{OK: false, Error: "перезапуск уже выполняется"})
						continue
					}
					if stopReply != nil {
						env.Reply(Response{OK: false, Error: "остановка уже выполняется"})
						continue
					}
					stopReply = env.Reply
					_ = store.update(func(s *State) { s.Status = "остановка" })
					cancelAttempt()
				case "restart":
					if stopReply != nil {
						env.Reply(Response{OK: false, Error: "остановка уже выполняется"})
						continue
					}
					if restartReply != nil {
						env.Reply(Response{OK: false, Error: "перезапуск уже выполняется"})
						continue
					}
					if pending != nil || switchRequested {
						env.Reply(Response{OK: false, Error: "нельзя перезапустить tunnelctl во время переключения профиля"})
						continue
					}
					executable, err := validateRestartExecutable(env.Request.Executable)
					if err != nil {
						env.Reply(Response{OK: false, Error: err.Error()})
						continue
					}
					restartExecutable = executable
					restartReply = env.Reply
					logx.Info("получена команда перезапуска tunnelctl")
					_ = store.update(func(s *State) { s.Status = "перезапуск" })
					cancelAttempt()
				case "switch":
					if stopReply != nil || restartReply != nil {
						env.Reply(Response{OK: false, Error: "остановка или перезапуск уже выполняется"})
						continue
					}
					if pending != nil || switchRequested {
						env.Reply(Response{OK: false, Error: "переключение уже выполняется"})
						continue
					}
					target, fallbackIndex, err := selectSwitchTarget(opts.Config, groupProfiles, isGroup, groupIndex, current, env.Request.Target)
					if err != nil {
						env.Reply(Response{OK: false, Error: err.Error()})
						continue
					}
					if target.Name == current.Name && healthy {
						env.Reply(Response{OK: true, Message: fmt.Sprintf("Профиль %s уже активен", current.Name), State: statePtr(store.snapshot())})
						continue
					}
					pending = &pendingSwitch{target: target, previous: current, fallbackIndex: fallbackIndex, reply: env.Reply}
					switchRequested = true
					fmt.Printf("Переключаю туннель: %s -> %s\n", current.Name, target.Name)
					logx.Info("ручное переключение: %s -> %s", current.Name, target.Name)
					_ = store.update(func(s *State) { s.Status = "ручное переключение" })
					cancelAttempt()
				default:
					env.Reply(Response{OK: false, Error: "неизвестная команда управления"})
				}

			case err := <-result:
				cancelAttempt()
				if restartReply != nil {
					if waitErr := sshproxy.WaitPortFree(context.Background(), current.EffectiveListen(opts.Config), 5*time.Second); waitErr != nil {
						restartReply(Response{OK: false, Error: waitErr.Error()})
						restartReply = nil
						restartExecutable = ""
						break attempt
					}
					_ = store.update(func(s *State) { s.Status = "запуск нового экземпляра" })
					restartReply(Response{OK: true, Message: "Команда перезапуска принята"})
					return &restartRequestedError{executable: restartExecutable}
				}
				if stopReply != nil {
					_ = store.update(func(s *State) { s.Status = "остановлен" })
					stopReply(Response{OK: true, Message: "Управляемый туннель остановлен"})
					return nil
				}
				if switchRequested {
					if waitErr := sshproxy.WaitPortFree(context.Background(), current.EffectiveListen(opts.Config), 5*time.Second); waitErr != nil {
						pending.reply(Response{OK: false, Error: waitErr.Error()})
						pending = nil
						switchRequested = false
						return waitErr
					}
					current = pending.target
					break attempt
				}
				if ctx.Err() != nil {
					return nil
				}
				if !opts.Watch {
					if err == nil {
						return nil
					}
					return err
				}

				if pending != nil {
					pending.reply(Response{OK: false, Error: fmt.Sprintf("профиль %s не прошёл проверку: %v", pending.target.Name, err)})
					if isGroup {
						groupIndex = (pending.fallbackIndex + 1) % len(groupProfiles)
						current = groupProfiles[groupIndex]
					} else {
						current = pending.previous
					}
					pending = nil
					manualActive = false
					break attempt
				}

				if isGroup {
					if manualActive {
						manualActive = false
						groupIndex = (groupIndex + 1) % len(groupProfiles)
						current = groupProfiles[groupIndex]
					} else {
						next := (groupIndex + 1) % len(groupProfiles)
						wrapped := next == 0
						groupIndex = next
						current = groupProfiles[groupIndex]
						delay := minDelay
						if wrapped {
							delay = cycleDelay
							cycleDelay *= 2
							if cycleDelay > maxDelay {
								cycleDelay = maxDelay
							}
						}
						if !sleepContext(ctx, delay) {
							return nil
						}
					}
				} else {
					if err != nil {
						fmt.Printf("Профиль %s завершился с ошибкой: %v\n", current.Name, err)
						logx.Warn("профиль %s завершился с ошибкой: %v", current.Name, err)
					}
					if !sleepContext(ctx, retryDelay) {
						return nil
					}
					retryDelay *= 2
					if retryDelay > maxDelay {
						retryDelay = maxDelay
					}
				}
				break attempt
			}
		}
	}
}

func selectSwitchTarget(cfg config.Config, group []config.Profile, isGroup bool, groupIndex int, current config.Profile, target string) (config.Profile, int, error) {
	if target == "next" {
		if !isGroup {
			return config.Profile{}, groupIndex, errors.New("нельзя выполнить switch next: текущий туннель запущен не из группы")
		}
		next := (groupIndex + 1) % len(group)
		return group[next], groupIndex, nil
	}
	p, ok := cfg.ResolveProfile(target)
	if !ok {
		return config.Profile{}, groupIndex, fmt.Errorf("профиль не найден: %s", target)
	}
	fallbackIndex := groupIndex
	if idx := profileIndex(group, current.Name); idx >= 0 {
		fallbackIndex = idx
	}
	return p, fallbackIndex, nil
}

func profileIndex(profiles []config.Profile, name string) int {
	for i, p := range profiles {
		if p.Name == name {
			return i
		}
	}
	return -1
}

func reconnectBounds(cfg config.Config) (time.Duration, time.Duration) {
	minDelay := time.Duration(cfg.Defaults.Reconnect.MinDelaySec) * time.Second
	maxDelay := time.Duration(cfg.Defaults.Reconnect.MaxDelaySec) * time.Second
	if minDelay <= 0 {
		minDelay = 2 * time.Second
	}
	if maxDelay < minDelay {
		maxDelay = minDelay
	}
	return minDelay, maxDelay
}

func sleepContext(ctx context.Context, delay time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(delay):
		return true
	}
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func statePtr(s State) *State { return &s }

// Status запрашивает состояние работающего супервизора.
func Status(ctx context.Context) (State, error) {
	resp, err := request(ctx, Request{Action: "status"})
	if err != nil {
		return State{}, err
	}
	if !resp.OK {
		return State{}, errors.New(resp.Error)
	}
	if resp.State == nil {
		return State{}, errors.New("управляющий процесс не вернул состояние")
	}
	return *resp.State, nil
}

// Stop корректно останавливает работающий супервизор.
func Stop(ctx context.Context) (string, error) {
	resp, err := request(ctx, Request{Action: "stop"})
	if err != nil {
		return "", err
	}
	if !resp.OK {
		return "", errors.New(resp.Error)
	}
	return resp.Message, nil
}

// Restart корректно завершает дочерний SSH и просит супервизор заменить себя указанным бинарником.
func Restart(ctx context.Context, executable string) (string, error) {
	resp, err := request(ctx, Request{Action: "restart", Executable: executable})
	if err != nil {
		return "", err
	}
	if !resp.OK {
		return "", errors.New(resp.Error)
	}
	return resp.Message, nil
}

// Switch переключает активный профиль работающего супервизора.
func Switch(ctx context.Context, target string) (State, string, error) {
	resp, err := request(ctx, Request{Action: "switch", Target: target})
	if err != nil {
		return State{}, "", err
	}
	if !resp.OK {
		return State{}, "", errors.New(resp.Error)
	}
	if resp.State == nil {
		return State{}, resp.Message, nil
	}
	return *resp.State, resp.Message, nil
}
