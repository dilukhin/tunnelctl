package io.github.dilukhin.tunnelctl.watchdog;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;

public final class BootReceiver extends BroadcastReceiver {
    @Override public void onReceive(Context context, Intent intent) {
        String action = intent == null ? null : intent.getAction();
        if (!Intent.ACTION_BOOT_COMPLETED.equals(action) && !Intent.ACTION_MY_PACKAGE_REPLACED.equals(action)) return;
        String event = Intent.ACTION_BOOT_COMPLETED.equals(action) ? "загрузки Android" : "обновления APK";
        if (!Settings.get(context).getBoolean("enabled", false)) {
            Settings.boot(context, "Получен сигнал " + event + "; наблюдение выключено.", false);
            return;
        }
        Settings.boot(context, "Получен сигнал " + event + "; запуск службы запрошен.", true);
        try {
            if (context.startForegroundService(new Intent(context, WatchdogService.class)
                    .setAction(WatchdogService.BOOT_START)) == null) {
                throw new IllegalStateException("service not found");
            }
        } catch (IllegalStateException | SecurityException e) {
            Settings.boot(context, "Android отклонил запуск после " + event + " ("
                    + e.getClass().getSimpleName() + ").", false);
            Settings.status(context, "Android запретил запуск. Откройте приложение и включите восстановление.");
        }
    }
}
