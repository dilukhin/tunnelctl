package io.github.dilukhin.tunnelctl.watchdog;

import android.annotation.SuppressLint;
import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.content.pm.ServiceInfo;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.IBinder;
import android.os.Looper;
import android.os.PowerManager;
import java.util.UUID;

public final class WatchdogService extends Service {
    static final String PERMISSION = "com.termux.permission.RUN_COMMAND";
    static final String STOP = "watchdog.STOP";
    private static final String CHANNEL = "recovery";
    private static WatchdogService instance;
    private final Handler handler = new Handler(Looper.getMainLooper());
    private final RetryPolicy retry = new RetryPolicy();
    private final Runnable tick = this::sendEnsure;
    private PowerManager.WakeLock wakeLock;
    private PendingIntent resultIntent;
    private String resultToken;
    private LaunchSpec spec;
    private boolean started;

    @Override public void onCreate() {
        super.onCreate();
        instance = this;
        getSystemService(NotificationManager.class).createNotificationChannel(
                new NotificationChannel(CHANNEL, "Восстановление туннеля", NotificationManager.IMPORTANCE_LOW));
    }

    @Override public int onStartCommand(Intent intent, int flags, int startId) {
        if (intent != null && STOP.equals(intent.getAction())) {
            Settings.get(this).edit().putBoolean("enabled", false).commit();
        }
        if (!Settings.get(this).getBoolean("enabled", false)) {
            stopSelf();
            return START_NOT_STICKY;
        }
        try {
            spec = Settings.spec(this);
            if (checkSelfPermission(PERMISSION) != PackageManager.PERMISSION_GRANTED) {
                throw new SecurityException();
            }
            Notification notification = notification("Восстановление включено");
            if (Build.VERSION.SDK_INT >= 34) {
                startForeground(1, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE);
            } else {
                startForeground(1, notification);
            }
            if (!started) {
                started = true;
                acquireWakeLock();
                handler.post(tick);
            }
            return START_STICKY;
        } catch (IllegalArgumentException | IllegalStateException | SecurityException e) {
            Settings.get(this).edit().putBoolean("enabled", false).commit();
            Settings.status(this, "Не удалось включить восстановление. Проверьте пути и разрешения.");
            stopSelf();
            return START_NOT_STICKY;
        }
    }

    @SuppressLint("WakelockTimeout")
    private void acquireWakeLock() {
        // Включён пользователем на всё время наблюдения. Освобождается в onDestroy
        // или системой при гибели процесса. Не является защитой от нехватки памяти.
        wakeLock = getSystemService(PowerManager.class).newWakeLock(
                PowerManager.PARTIAL_WAKE_LOCK, getPackageName() + ":recovery");
        wakeLock.acquire();
    }

    private Notification notification(String text) {
        PendingIntent open = PendingIntent.getActivity(this, 0, new Intent(this, MainActivity.class),
                PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);
        PendingIntent stop = PendingIntent.getService(this, 1,
                new Intent(this, WatchdogService.class).setAction(STOP),
                PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);
        return new Notification.Builder(this, CHANNEL)
                .setSmallIcon(R.drawable.ic_tunnel).setContentTitle("tunnelctl")
                .setContentText(text).setContentIntent(open).setOngoing(true).setOnlyAlertOnce(true)
                .addAction(new Notification.Action.Builder(null, "Выключить восстановление", stop).build())
                .build();
    }

    private void sendEnsure() {
        if (!Settings.get(this).getBoolean("enabled", false)) { stopSelf(); return; }
        if (resultIntent != null) resultIntent.cancel();
        resultToken = "tunnelctl-result:" + UUID.randomUUID();
        Intent callback = new Intent(this, ResultReceiver.class).setData(Uri.parse(resultToken));
        int flags = PendingIntent.FLAG_ONE_SHOT;
        if (Build.VERSION.SDK_INT >= 31) flags |= PendingIntent.FLAG_MUTABLE;
        resultIntent = PendingIntent.getBroadcast(this, 0, callback, flags);
        Intent command = new Intent("com.termux.RUN_COMMAND")
                .setClassName("com.termux", "com.termux.app.RunCommandService")
                .putExtra("com.termux.RUN_COMMAND_PATH", spec.binary)
                .putExtra("com.termux.RUN_COMMAND_ARGUMENTS", spec.arguments())
                .putExtra("com.termux.RUN_COMMAND_WORKDIR", LaunchSpec.HOME)
                .putExtra("com.termux.RUN_COMMAND_BACKGROUND", true)
                .putExtra("com.termux.RUN_COMMAND_PENDING_INTENT", resultIntent);
        try {
            if (startService(command) == null) throw new IllegalStateException();
            show("Запрос передан Termux; ожидается подтверждение владельца.");
            // Первый ensure становится долгоживущим владельцем. Его ответа ждать
            // нельзя: следующий ensure подтвердит владельца через управляющий канал.
            schedule(RetryPolicy.NORMAL_MS);
        } catch (IllegalStateException | SecurityException e) {
            show("Termux недоступен. Проверьте разрешение запуска и ограничения батареи.");
            schedule(retry.failure());
        }
    }

    static void deliverResult(String token, Bundle result) {
        WatchdogService service = instance;
        if (service == null || !token.equals(service.resultToken)
                || !Settings.get(service).getBoolean("enabled", false)) return;
        // Не сохраняем и не показываем stdout/stderr/errmsg, кроме фиксированной метки.
        boolean managed = result != null && result.getInt("err", -1) == -1
                && result.getInt("exitCode", -1) == 0
                && "TUNNELCTL_ENSURE_MANAGED_V1".equals(result.getString("stdout", "").trim());
        if (managed) {
            service.retry.success();
            service.show("Владелец tunnelctl подтверждён. Проверку SOCKS выполняет tunnelctl.");
            service.schedule(RetryPolicy.NORMAL_MS);
        } else {
            service.show("Нет подтверждения владельца. Проверьте tunnelctl ensure в Termux.");
            service.schedule(service.retry.failure());
        }
    }

    private void schedule(long delay) {
        handler.removeCallbacks(tick);
        handler.postDelayed(tick, delay);
    }

    @SuppressLint("MissingPermission")
    private void show(String text) {
        Settings.status(this, text);
        getSystemService(NotificationManager.class).notify(1, notification(text));
    }

    @Override public void onDestroy() {
        handler.removeCallbacksAndMessages(null);
        if (resultIntent != null) resultIntent.cancel();
        if (wakeLock != null && wakeLock.isHeld()) wakeLock.release();
        if (instance == this) instance = null;
        stopForeground(STOP_FOREGROUND_REMOVE);
        super.onDestroy();
    }

    @Override public IBinder onBind(Intent intent) { return null; }
}
