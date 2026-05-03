package main

import "testing"

func TestStringSettingFlagOverridesEnv(t *testing.T) {
	t.Setenv("AUTOTEST_REPO_PATH", "from-env")
	got := stringSetting([]string{"autotest", "run", "--repo-path", "from-flag"}, "--repo-path", "AUTOTEST_REPO_PATH", ".")
	if got != "from-flag" {
		t.Fatalf("expected flag value, got %q", got)
	}
}

func TestStringSettingUsesEnv(t *testing.T) {
	t.Setenv("AUTOTEST_REPO_PATH", "from-env")
	got := stringSetting([]string{"autotest"}, "--repo-path", "AUTOTEST_REPO_PATH", ".")
	if got != "from-env" {
		t.Fatalf("expected env value, got %q", got)
	}
}

func TestBoolSettingParsesCommonValues(t *testing.T) {
	t.Setenv("AUTOTEST_WATCH", "yes")
	if !boolSetting([]string{"autotest"}, "--watch", "AUTOTEST_WATCH", false) {
		t.Fatal("expected yes to enable bool setting")
	}
	if boolSetting([]string{"autotest", "--watch", "off"}, "--watch", "AUTOTEST_WATCH", true) {
		t.Fatal("expected flag off to override env")
	}
}

func TestIntSettingFlagOverridesEnv(t *testing.T) {
	t.Setenv("AUTOTEST_SLOW_MO_MS", "900")
	got := intSetting([]string{"autotest", "--slow-mo", "300"}, "--slow-mo", "AUTOTEST_SLOW_MO_MS", 0)
	if got != 300 {
		t.Fatalf("expected flag int, got %d", got)
	}
}
