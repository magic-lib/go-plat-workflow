package service

import (
	"errors"
	"testing"
)

func TestDropTableIfEmpty(t *testing.T) {
	cases := []struct {
		name      string
		has       bool
		rows      int64
		countErr  error
		wantDrop  bool
		wantError bool
	}{
		{name: "表不存在跳过", has: false},
		{name: "有数据只告警不删", has: true, rows: 3},
		{name: "空表删除", has: true, rows: 0, wantDrop: true},
		{name: "统计失败返回错误", has: true, countErr: errors.New("boom"), wantError: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dropped, counted := false, false
			err := dropTableIfEmpty("wf_x",
				func() bool { return c.has },
				func() (int64, error) { counted = true; return c.rows, c.countErr },
				func() error { dropped = true; return nil },
			)
			if (err != nil) != c.wantError {
				t.Fatalf("err = %v, wantError %v", err, c.wantError)
			}
			if dropped != c.wantDrop {
				t.Fatalf("dropped = %v, want %v", dropped, c.wantDrop)
			}
			if !c.has && counted {
				t.Fatal("should not count when table missing")
			}
		})
	}
}

func TestDropTableIfEmptyDropError(t *testing.T) {
	err := dropTableIfEmpty("wf_x",
		func() bool { return true },
		func() (int64, error) { return 0, nil },
		func() error { return errors.New("denied") },
	)
	if err == nil {
		t.Fatal("drop error should be returned")
	}
}
