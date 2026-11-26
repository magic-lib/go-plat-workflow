package workflow

import (
	"encoding/json"
	"testing"
)

func TestRedisConfigForWorker(t *testing.T) {
	var nilCfg *RedisConfig
	if nilCfg.ForWorker() != nil {
		t.Fatal("nil config should return nil")
	}

	plain := &RedisConfig{Addr: "1.2.3.4:6379", Password: "p", DB: 2, Username: "u"}
	got := plain.ForWorker()
	if got == plain {
		t.Fatal("ForWorker must return a copy")
	}
	if *got != *plain {
		t.Fatalf("without worker_addr should be unchanged, got %+v", got)
	}

	withWorker := &RedisConfig{Addr: "1.2.3.4:6379", Password: "p", DB: 2, Username: "u", WorkerAddr: " 10.0.0.1:6379 "}
	got = withWorker.ForWorker()
	if got.Addr != "10.0.0.1:6379" {
		t.Fatalf("addr should be replaced by worker_addr, got %q", got.Addr)
	}
	if got.Password != "p" || got.DB != 2 || got.Username != "u" {
		t.Fatalf("other fields should be kept, got %+v", got)
	}
	if withWorker.Addr != "1.2.3.4:6379" {
		t.Fatal("ForWorker must not modify the original")
	}

	blank := &RedisConfig{Addr: "1.2.3.4:6379", WorkerAddr: "   "}
	if blank.ForWorker().Addr != "1.2.3.4:6379" {
		t.Fatal("blank worker_addr should be ignored")
	}
}

func TestRedisConfigWorkerAddrJSON(t *testing.T) {
	var cfg RedisConfig
	if err := json.Unmarshal([]byte(`{"addr":"1.2.3.4:6379","db":0}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.WorkerAddr != "" {
		t.Fatalf("old json without worker_addr should decode to empty, got %q", cfg.WorkerAddr)
	}
	b, err := json.Marshal(&RedisConfig{Addr: "a:1", WorkerAddr: "b:2"})
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back["worker_addr"] != "b:2" {
		t.Fatalf("worker_addr should be encoded, got %s", b)
	}
}

func TestEnvConfigMaskKeepsWorkerAddr(t *testing.T) {
	stored := &EnvConfigDef{RedisConfig: &RedisConfig{Addr: "r:6379", Password: "rpwd", WorkerAddr: "10.0.0.1:6379"}}
	masked := stored.MaskSecrets()
	if masked.RedisConfig.WorkerAddr != "10.0.0.1:6379" {
		t.Fatalf("worker_addr should be shown, got %q", masked.RedisConfig.WorkerAddr)
	}

	// 只改 worker_addr 时，脱敏密码应沿用库里的值
	incoming := &EnvConfigDef{RedisConfig: &RedisConfig{Addr: "r:6379", Password: MaskedSecret, WorkerAddr: "10.0.0.2:6379"}}
	incoming.RestoreMaskedSecrets(stored)
	if incoming.RedisConfig.Password != "rpwd" {
		t.Fatalf("password should be restored, got %q", incoming.RedisConfig.Password)
	}
}
