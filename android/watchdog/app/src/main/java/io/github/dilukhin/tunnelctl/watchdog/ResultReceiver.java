package io.github.dilukhin.tunnelctl.watchdog;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;

// Получатель закрыт; Termux получает одноразовый PendingIntent только для своего ответа.
public final class ResultReceiver extends BroadcastReceiver {
    @Override public void onReceive(Context context, Intent intent) {
        if (intent.getData() != null) {
            WatchdogService.deliverResult(intent.getData().toString(), intent.getBundleExtra("result"));
        }
    }
}
