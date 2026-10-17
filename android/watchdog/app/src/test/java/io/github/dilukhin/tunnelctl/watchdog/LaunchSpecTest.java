package io.github.dilukhin.tunnelctl.watchdog;

import org.junit.Test;
import static org.junit.Assert.*;

public class LaunchSpecTest {
    @Test public void passesArgumentsWithoutShell() {
        LaunchSpec spec = new LaunchSpec(LaunchSpec.DEFAULT_BINARY, LaunchSpec.HOME + "/my config.json", "авто");
        assertArrayEquals(new String[]{"ensure", "авто", "--config", LaunchSpec.HOME + "/my config.json"}, spec.arguments());
    }
    @Test public void rejectsUnsafePathsAndOptions() {
        for (String path : new String[]{"/sdcard/tunnelctl", LaunchSpec.HOME + "/../tunnelctl",
                LaunchSpec.HOME + "/tunnelctl\n", LaunchSpec.HOME + "/bash"}) {
            assertThrows(IllegalArgumentException.class, () -> new LaunchSpec(path, LaunchSpec.DEFAULT_CONFIG, "auto"));
        }
        for (String name : new String[]{"--help", "auto;sh", "auto\nx", ""}) {
            assertThrows(IllegalArgumentException.class, () -> new LaunchSpec(LaunchSpec.DEFAULT_BINARY, LaunchSpec.DEFAULT_CONFIG, name));
        }
    }
    @Test public void errorDoesNotEchoInput() {
        Exception error = assertThrows(IllegalArgumentException.class,
                () -> new LaunchSpec("/sdcard/private-value", LaunchSpec.DEFAULT_CONFIG, "auto"));
        assertFalse(error.getMessage().contains("private-value"));
    }
}
