//go:build windows

package main

import "context"

type adapterScanResult struct {
	adapters []Adapter
	err      error
}

type adapterScanJob struct {
	token  uintptr
	cancel context.CancelFunc
	result chan adapterScanResult
}

func startAdapterScan(token uintptr, scan func(context.Context) ([]Adapter, error), notify func()) *adapterScanJob {
	ctx, cancel := context.WithCancel(context.Background())
	job := &adapterScanJob{token: token, cancel: cancel, result: make(chan adapterScanResult, 1)}
	go func() {
		a, err := scan(ctx)
		job.result <- adapterScanResult{a, err}
		if ctx.Err() == nil {
			notify()
		}
	}()
	return job
}
