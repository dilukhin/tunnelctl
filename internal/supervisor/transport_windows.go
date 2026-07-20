//go:build windows

package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"
)

const (
	pipeName                  = `\\.\pipe\tunnelctl-control`
	pipeAccessDuplex          = 0x00000003
	fileFlagFirstPipeInstance = 0x00080000
	pipeTypeMessage           = 0x00000004
	pipeReadModeMessage       = 0x00000002
	pipeWait                  = 0x00000000
	pipeRejectRemoteClients   = 0x00000008
	pipeUnlimitedInstances    = 255
	errorPipeConnected        = syscall.Errno(535)
)

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procCreateNamedPipeW = kernel32.NewProc("CreateNamedPipeW")
	procConnectNamedPipe = kernel32.NewProc("ConnectNamedPipe")
	procDisconnectPipe   = kernel32.NewProc("DisconnectNamedPipe")
	procWaitNamedPipeW   = kernel32.NewProc("WaitNamedPipeW")
)

type windowsServer struct {
	file     *os.File
	handle   syscall.Handle
	requests chan controlEnvelope
	done     chan struct{}
}

func platformListen() (controlServer, error) {
	name, err := syscall.UTF16PtrFromString(pipeName)
	if err != nil {
		return nil, err
	}
	handle, _, callErr := procCreateNamedPipeW.Call(
		uintptr(unsafe.Pointer(name)),
		uintptr(pipeAccessDuplex|fileFlagFirstPipeInstance),
		uintptr(pipeTypeMessage|pipeReadModeMessage|pipeWait|pipeRejectRemoteClients),
		uintptr(pipeUnlimitedInstances),
		64*1024,
		64*1024,
		0,
		0,
	)
	if handle == uintptr(syscall.InvalidHandle) {
		return nil, fmt.Errorf("не удалось создать именованный канал tunnelctl: %w", callErr)
	}
	h := syscall.Handle(handle)
	s := &windowsServer{
		file:     os.NewFile(handle, pipeName),
		handle:   h,
		requests: make(chan controlEnvelope),
		done:     make(chan struct{}),
	}
	go s.acceptLoop()
	return s, nil
}

func (s *windowsServer) Requests() <-chan controlEnvelope { return s.requests }

func (s *windowsServer) Close() error {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
	_ = syscall.CancelIo(s.handle)
	return s.file.Close()
}

func (s *windowsServer) acceptLoop() {
	defer close(s.requests)
	for {
		ok, _, err := procConnectNamedPipe.Call(uintptr(s.handle), 0)
		if ok == 0 && err != errorPipeConnected {
			select {
			case <-s.done:
				return
			default:
				time.Sleep(100 * time.Millisecond)
				continue
			}
		}
		s.handleConnection()
		procDisconnectPipe.Call(uintptr(s.handle))
		select {
		case <-s.done:
			return
		default:
		}
	}
}

func (s *windowsServer) handleConnection() {
	var req Request
	if err := json.NewDecoder(s.file).Decode(&req); err != nil {
		_ = json.NewEncoder(s.file).Encode(Response{OK: false, Error: "не удалось прочитать запрос управления"})
		return
	}
	replied := make(chan struct{})
	envelope := controlEnvelope{
		Request: req,
		Reply: func(resp Response) {
			_ = json.NewEncoder(s.file).Encode(resp)
			close(replied)
		},
	}
	select {
	case s.requests <- envelope:
		<-replied
	case <-s.done:
		_ = json.NewEncoder(s.file).Encode(Response{OK: false, Error: "управляющий канал остановлен"})
	}
}

func platformRequest(ctx context.Context, req Request) (Response, error) {
	name, err := syscall.UTF16PtrFromString(pipeName)
	if err != nil {
		return Response{}, err
	}
	timeout := uint32(5000)
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return Response{}, context.DeadlineExceeded
		}
		timeout = uint32(remaining / time.Millisecond)
	}
	ok, _, waitErr := procWaitNamedPipeW.Call(uintptr(unsafe.Pointer(name)), uintptr(timeout))
	if ok == 0 {
		return Response{}, fmt.Errorf("не удалось подключиться к управляющему каналу tunnelctl: %w", waitErr)
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		return Response{}, fmt.Errorf("не удалось открыть именованный канал tunnelctl: %w", err)
	}
	f := os.NewFile(uintptr(handle), pipeName)
	defer f.Close()
	req.Version = protocolVersion
	if err := json.NewEncoder(f).Encode(req); err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.NewDecoder(f).Decode(&resp); err != nil {
		return Response{}, err
	}
	if !resp.OK && resp.Error == "" {
		return Response{}, errors.New("управляющий процесс вернул неизвестную ошибку")
	}
	return resp, nil
}
