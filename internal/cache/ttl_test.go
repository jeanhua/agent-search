package cache

import (
	"testing"
	"time"
)

func TestSetGet(t *testing.T) {
	c := New(2, time.Minute)
	c.Set("a", "1")
	if v, ok := c.Get("a"); !ok || v != "1" {
		t.Fatalf("expected 1, got %q ok=%v", v, ok)
	}
	if _, ok := c.Get("missing"); ok {
		t.Fatal("expected miss for missing key")
	}
}

func TestExpiry(t *testing.T) {
	c := New(10, 50*time.Millisecond)
	c.Set("a", "1")
	if _, ok := c.Get("a"); !ok {
		t.Fatal("expected hit before expiry")
	}
	time.Sleep(80 * time.Millisecond)
	if _, ok := c.Get("a"); ok {
		t.Fatal("expected miss after expiry")
	}
}

func TestEviction(t *testing.T) {
	c := New(2, time.Minute)
	c.Set("a", "1")
	c.Set("b", "2")
	c.Set("c", "3") // 应淘汰 a（最久未使用）
	if _, ok := c.Get("a"); ok {
		t.Fatal("expected a evicted")
	}
	if _, ok := c.Get("b"); !ok {
		t.Fatal("expected b present")
	}
	if _, ok := c.Get("c"); !ok {
		t.Fatal("expected c present")
	}
}

func TestLRUTouch(t *testing.T) {
	c := New(2, time.Minute)
	c.Set("a", "1")
	c.Set("b", "2")
	c.Get("a") // 刷新 a
	c.Set("c", "3")
	if _, ok := c.Get("a"); !ok {
		t.Fatal("expected a kept after touch")
	}
	if _, ok := c.Get("b"); ok {
		t.Fatal("expected b evicted")
	}
}
