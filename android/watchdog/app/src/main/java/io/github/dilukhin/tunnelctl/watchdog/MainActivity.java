package io.github.dilukhin.tunnelctl.watchdog;

import android.Manifest;
import android.app.Activity;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.PackageManager;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.provider.Settings;
import android.text.InputType;
import android.view.View;
import android.widget.Button;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;
import android.widget.Toast;
import java.text.DateFormat;
import java.util.ArrayList;
import java.util.Date;

public final class MainActivity extends Activity {
    private SharedPreferences prefs;
    private EditText binary, config, target;
    private TextView state;
    private Button enable;
    private final SharedPreferences.OnSharedPreferenceChangeListener listener = (p, k) -> refresh();

    @Override public void onCreate(Bundle savedState) {
        super.onCreate(savedState);
        prefs = io.github.dilukhin.tunnelctl.watchdog.Settings.get(this);
        LinearLayout column = new LinearLayout(this);
        column.setOrientation(LinearLayout.VERTICAL);
        int pad = (int) (20 * getResources().getDisplayMetrics().density);
        column.setPadding(pad, pad, pad, pad);
        // На Android 15 учитываем системные панели при обязательном edge-to-edge.
        column.setOnApplyWindowInsetsListener((view, insets) -> {
            view.setPadding(pad + insets.getSystemWindowInsetLeft(), pad + insets.getSystemWindowInsetTop(),
                    pad + insets.getSystemWindowInsetRight(), pad + insets.getSystemWindowInsetBottom());
            return insets;
        });
        ScrollView scroll = new ScrollView(this);
        scroll.addView(column);
        setContentView(scroll);
        text(column, "tunnelctl — восстановление", 24);
        text(column, "Включите наблюдение, чтобы заново запускать tunnelctl после завершения Termux системой.", 16);
        state = text(column, "", 16);
        binary = field(column, "Путь к tunnelctl", prefs.getString("binary", LaunchSpec.DEFAULT_BINARY));
        config = field(column, "Путь к конфигурации", prefs.getString("config", LaunchSpec.DEFAULT_CONFIG));
        target = field(column, "Профиль или группа", prefs.getString("target", "auto"));
        enable = button(column, "Включить восстановление", v -> start());
        button(column, "Выключить восстановление", v -> {
            prefs.edit().putBoolean("enabled", false).commit();
            stopService(new Intent(this, WatchdogService.class));
            io.github.dilukhin.tunnelctl.watchdog.Settings.status(this,
                    "Восстановление выключено. Уже работающий туннель можно остановить в Termux.");
            refresh();
        });
        button(column, "Разрешения приложения", v -> startActivity(new Intent(
                Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.parse("package:" + getPackageName()))));
        button(column, "Настройки батареи", v -> startActivity(new Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS)));
        text(column, "Подготовка в Termux: установите tunnelctl с командой ensure, проверьте подключение с ключом без запроса пароля. "
                + "В ~/.termux/termux.properties включите allow-external-apps=true. Разрешение запуска выдавайте только доверенным приложениям.", 14);
        text(column, "Отключите ограничения батареи для Termux и этого приложения. Наблюдение удерживает процессор от сна и расходует батарею. "
                + "Android и прошивка могут задержать восстановление. После принудительной остановки откройте приложение вручную.", 14);
        text(column, "Проверка связи: tunnelctl status в Termux. Уведомление подтверждает владельца процесса, а не доступность Интернета.", 14);
        refresh();
    }

    private TextView text(LinearLayout column, String value, int size) {
        TextView view = new TextView(this);
        view.setText(value); view.setTextSize(size); view.setPadding(0, 10, 0, 10);
        column.addView(view);
        return view;
    }
    private EditText field(LinearLayout column, String label, String value) {
        text(column, label, 14);
        EditText field = new EditText(this);
        field.setText(value); field.setSingleLine(true);
        field.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS);
        column.addView(field);
        return field;
    }
    private Button button(LinearLayout column, String label, View.OnClickListener click) {
        Button button = new Button(this);
        button.setText(label); button.setOnClickListener(click); column.addView(button);
        return button;
    }
    private void start() {
        try { getPackageManager().getPackageInfo("com.termux", 0); }
        catch (PackageManager.NameNotFoundException e) { message("Сначала установите и откройте Termux."); return; }
        ArrayList<String> permissions = new ArrayList<>();
        if (checkSelfPermission(WatchdogService.PERMISSION) != PackageManager.PERMISSION_GRANTED) permissions.add(WatchdogService.PERMISSION);
        if (Build.VERSION.SDK_INT >= 33 && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED)
            permissions.add(Manifest.permission.POST_NOTIFICATIONS);
        if (!permissions.isEmpty()) {
            requestPermissions(permissions.toArray(new String[0]), 1);
            message("После выдачи разрешений нажмите «Включить восстановление» ещё раз.");
            return;
        }
        try {
            LaunchSpec spec = new LaunchSpec(binary.getText().toString().trim(),
                    config.getText().toString().trim(), target.getText().toString().trim());
            prefs.edit().putString("binary", spec.binary).putString("config", spec.config)
                    .putString("target", spec.target).putBoolean("enabled", true).commit();
            startForegroundService(new Intent(this, WatchdogService.class));
            refresh();
        } catch (IllegalArgumentException | IllegalStateException | SecurityException e) {
            prefs.edit().putBoolean("enabled", false).commit();
            message("Не удалось включить восстановление. Проверьте пути и разрешения.");
        }
    }
    private void refresh() {
        boolean enabled = prefs.getBoolean("enabled", false);
        boolean active = WatchdogService.isActive();
        String time = prefs.getLong("updated", 0) == 0 ? "" :
                "\n" + DateFormat.getDateTimeInstance().format(new Date(prefs.getLong("updated", 0)));
        String boot = prefs.getLong("boot_updated", 0) == 0 ? "\nСигнал загрузки ещё не зарегистрирован." :
                "\nПоследний запуск после загрузки/обновления: "
                + prefs.getString("boot_status", "нет сведений") + "\n"
                + DateFormat.getDateTimeInstance().format(new Date(prefs.getLong("boot_updated", 0)));
        String mode = !enabled ? "Восстановление выключено\n" : active
                ? "Восстановление включено, служба работает\n"
                : "Восстановление разрешено, но служба не работает. Нажмите «Включить восстановление».\n";
        state.setText(mode + prefs.getString("status", "Настройте пути и разрешения.") + time + boot);
        binary.setEnabled(!enabled); config.setEnabled(!enabled); target.setEnabled(!enabled);
        // Повторное нажатие позволяет восстановиться после отказа запуска на загрузке.
        enable.setEnabled(true);
    }
    private void message(String text) { Toast.makeText(this, text, Toast.LENGTH_LONG).show(); }
    @Override protected void onResume() { super.onResume(); prefs.registerOnSharedPreferenceChangeListener(listener); refresh(); }
    @Override protected void onPause() { prefs.unregisterOnSharedPreferenceChangeListener(listener); super.onPause(); }
}
