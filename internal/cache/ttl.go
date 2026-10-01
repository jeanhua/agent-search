// Package cache 提供简单的 TTL 内存缓存（固定容量 + 过期淘汰）。
package cache

import (
	"container/list"
	"sync"
	"time"
)

type entry struct {
	key     string
	value   string
	expires time.Time
}

// TTL 是并发安全、有过期时间的 LRU 风格内存缓存。
type TTL struct {
	mu      sync.Mutex
	maxSize int
	ttl     time.Duration
	items   map[string]*list.Element
	lru     *list.List // 队首为最近使用，队尾为最久未使用
}

// New 创建容量为 maxSize、有效期 ttl 的缓存。
func New(maxSize int, ttl time.Duration) *TTL {
	if maxSize <= 0 {
		maxSize = 1
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &TTL{
		maxSize: maxSize,
		ttl:     ttl,
		items:   make(map[string]*list.Element, maxSize),
		lru:     list.New(),
	}
}

// Get 返回 key 对应的值；已过期或不存在时 ok 为 false。
func (c *TTL) Get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.items[key]
	if !ok {
		return "", false
	}
	e := el.Value.(*entry)
	if time.Now().After(e.expires) {
		c.removeElement(el)
		return "", false
	}
	c.lru.MoveToFront(el)
	return e.value, true
}

// Set 写入 key/value，容量满时淘汰最久未使用且已过期的条目。
func (c *TTL) Set(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.items[key]; ok {
		e := el.Value.(*entry)
		e.value = value
		e.expires = time.Now().Add(c.ttl)
		c.lru.MoveToFront(el)
		return
	}

	// 先清理过期项，避免误删热数据。
	for el := c.lru.Back(); el != nil; {
		prev := el.Prev()
		e := el.Value.(*entry)
		if time.Now().After(e.expires) {
			c.removeElement(el)
		}
		el = prev
	}
	if len(c.items) >= c.maxSize {
		c.removeElement(c.lru.Back())
	}

	e := &entry{key: key, value: value, expires: time.Now().Add(c.ttl)}
	c.items[key] = c.lru.PushFront(e)
}

func (c *TTL) removeElement(el *list.Element) {
	if el == nil {
		return
	}
	c.lru.Remove(el)
	delete(c.items, el.Value.(*entry).key)
}

// Len 返回当前条目数（含未清理的过期项）。
func (c *TTL) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}
