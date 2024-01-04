package internal

import (
	"context"
	"sync"
)

type SupervisedService interface {
	Start(ctx context.Context) error
}

type Supervisor struct {
	services []SupervisedService
}

func NewSupervisor(svc ...SupervisedService) *Supervisor {
	return &Supervisor{
		services: svc,
	}
}

func (s *Supervisor) Add(svc SupervisedService) *Supervisor {
	s.services = append(s.services, svc)

	return s
}

func (s *Supervisor) Start(ctx context.Context) error {
	var wg sync.WaitGroup
	cancelCtx, cancel := context.WithCancel(ctx)

	errCh := make(chan error, len(s.services))

	for _, svc := range s.services {
		go func(svc SupervisedService) {
			wg.Add(1)
			errCh <- svc.Start(cancelCtx)
			wg.Done()
		}(svc)
	}

	firstErr := <-errCh
	cancel()
	wg.Wait()

	return firstErr
}
