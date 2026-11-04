package workflow

import "testing"

func TestEnvConfigMaskAndRestore(t *testing.T) {
	stored := &EnvConfigDef{
		RedisConfig: &RedisConfig{Addr: "r:6379", Password: "rpwd"},
		MySQLConfig: &MySQLConfig{Host: "m", Password: "mpwd", DSN: "u:mpwd@tcp(m)/db"},
	}
	masked := stored.MaskSecrets()
	if masked.RedisConfig.Password != MaskedSecret || masked.MySQLConfig.Password != MaskedSecret || masked.MySQLConfig.DSN != MaskedSecret {
		t.Fatalf("secrets not masked: %+v %+v", masked.RedisConfig, masked.MySQLConfig)
	}
	if stored.RedisConfig.Password != "rpwd" || stored.MySQLConfig.DSN == MaskedSecret {
		t.Fatal("MaskSecrets must not modify the original")
	}
	if (&EnvConfigDef{RedisConfig: &RedisConfig{Addr: "x"}}).MaskSecrets().RedisConfig.Password != "" {
		t.Fatal("empty password should stay empty")
	}

	// 前端原样回传脱敏值：还原为库里的真实值；改了的字段保留新值
	posted := masked.MaskSecrets()
	posted.MySQLConfig.Password = "new"
	posted.RestoreMaskedSecrets(stored)
	if posted.RedisConfig.Password != "rpwd" || posted.MySQLConfig.Password != "new" || posted.MySQLConfig.DSN != stored.MySQLConfig.DSN {
		t.Fatalf("restore wrong: %+v %+v", posted.RedisConfig, posted.MySQLConfig)
	}

	// 库里没有记录时，脱敏值不能被当成密码写进库
	fresh := &EnvConfigDef{RedisConfig: &RedisConfig{Password: MaskedSecret}}
	fresh.RestoreMaskedSecrets(nil)
	if fresh.RedisConfig.Password != "" {
		t.Fatalf("want empty, got %q", fresh.RedisConfig.Password)
	}
}
