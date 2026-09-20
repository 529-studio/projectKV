package ws

import "sync"

type Hub struct{ mu sync.Mutex }

func NewHub() *Hub { return &Hub{} }
