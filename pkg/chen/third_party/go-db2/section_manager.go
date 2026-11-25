package db2

import (
	"errors"
	"sync"
)

const (
	maxDrdaSections   = 500
	reservedDirectSec = 1 // Seção estática para ExecDirect / QueryDirect
)

// sectionManager gerencia a atribuição de seções DRDA utilizando um bitmap de 512 bits.
// Garante 0 alocações no heap e complexidade O(1) na liberação.
type sectionManager struct {
	mu       sync.Mutex
	bitmap   [8]uint64 // 8 * 64 bits = 512 seções disponíveis
	nextFree uint16
	maxSec   uint16
}

func newSectionManager(maxSections uint16) *sectionManager {
	if maxSections == 0 || maxSections > maxDrdaSections {
		maxSections = maxDrdaSections
	}
	sm := &sectionManager{
		nextFree: 2, // Seção 1 é reservada para comandos imediatos
		maxSec:   maxSections,
	}
	// Marca a seção 0 (inválida) e a seção 1 (reservada) como ocupadas
	sm.bitmap[0] |= (1 << 0) | (1 << 1)
	return sm
}

func (sm *sectionManager) Acquire() (uint16, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	total := sm.maxSec - 1 // Faixa [2, maxSec]
	for i := uint16(0); i < total; i++ {
		sec := sm.nextFree
		sm.nextFree++
		if sm.nextFree > sm.maxSec {
			sm.nextFree = 2
		}

		word := sec / 64
		bit := sec % 64
		if (sm.bitmap[word] & (1 << bit)) == 0 {
			sm.bitmap[word] |= (1 << bit)
			return sec, nil
		}
	}
	return 0, errors.New("db2: maximum concurrent prepared statements exceeded for connection")
}

func (sm *sectionManager) Release(sec uint16) {
	if sec <= 1 || sec > sm.maxSec {
		return
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()

	word := sec / 64
	bit := sec % 64
	sm.bitmap[word] &^= (1 << bit)
}

func (sm *sectionManager) isAllocated(sec uint16) bool {
	if sec > sm.maxSec {
		return false
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()

	word := sec / 64
	bit := sec % 64
	return (sm.bitmap[word] & (1 << bit)) != 0
}
