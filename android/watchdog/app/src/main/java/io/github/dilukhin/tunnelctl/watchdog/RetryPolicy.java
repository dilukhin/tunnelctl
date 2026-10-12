package io.github.dilukhin.tunnelctl.watchdog;

final class RetryPolicy {
    static final long NORMAL_MS = 60_000;
    static final long MAX_MS = 15 * 60_000;
    private long delay = NORMAL_MS;
    long failure() { delay = Math.min(MAX_MS, delay * 2); return delay; }
    void success() { delay = NORMAL_MS; }
}
