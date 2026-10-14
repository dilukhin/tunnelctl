package io.github.dilukhin.tunnelctl.watchdog;

import android.content.Context;
import android.content.SharedPreferences;

final class Settings {
    static SharedPreferences get(Context context) {
        return context.getSharedPreferences("watchdog", Context.MODE_PRIVATE);
    }
    static LaunchSpec spec(Context context) {
        SharedPreferences p = get(context);
        return new LaunchSpec(p.getString("binary", LaunchSpec.DEFAULT_BINARY),
                p.getString("config", LaunchSpec.DEFAULT_CONFIG), p.getString("target", "auto"));
    }
    static void status(Context context, String text) {
        get(context).edit().putString("status", text).putLong("updated", System.currentTimeMillis()).apply();
    }
    static void boot(Context context, String text, boolean pending) {
        // Приёмник может завершиться сразу после onReceive: сохраняем факт доставки синхронно.
        get(context).edit().putString("boot_status", text)
                .putLong("boot_updated", System.currentTimeMillis())
                .putBoolean("boot_pending", pending).commit();
    }
}
