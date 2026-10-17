package io.github.dilukhin.tunnelctl.watchdog;

import org.junit.Test;
import static org.junit.Assert.*;

public class RetryPolicyTest {
    @Test public void capsFailuresAndResetsAfterSuccess() {
        RetryPolicy policy = new RetryPolicy();
        assertEquals(120_000, policy.failure());
        for (int i = 0; i < 100; i++) assertTrue(policy.failure() <= RetryPolicy.MAX_MS);
        assertEquals(RetryPolicy.MAX_MS, policy.failure());
        policy.success();
        assertEquals(120_000, policy.failure());
    }
}
