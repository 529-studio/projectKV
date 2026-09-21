package ws

import "sync"

type Hub struct {
	mu sync.Mutex //nolint:unused
}

func NewHub() *Hub { return &Hub{} }
