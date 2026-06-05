package util

import "testing"

func TestNextIDTracksCountersPerPrefix(t *testing.T) {
	// 不同业务对象共享数字后缀会让知识库编号被文档、日志等对象跳号。
	ObserveID("unit-kb-9000")
	ObserveID("unit-doc-12000")

	if got := NextID("unit-kb"); got != "unit-kb-9001" {
		t.Fatalf("expected unit-kb counter to advance independently, got %s", got)
	}
	if got := NextID("unit-doc"); got != "unit-doc-12001" {
		t.Fatalf("expected unit-doc counter to advance independently, got %s", got)
	}
}
