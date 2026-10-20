package data_svc

import (
	"context"
	"fmt"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/model"
	"sync"
	"time"
)

type selectOraclePoolEntry struct {
	adapter   *reportoracle.Adapter
	version   string
	readers   int
	retired   bool
	updatedAt time.Time
}
type selectOraclePool struct {
	sync.Mutex
	entries map[uint]*selectOraclePoolEntry
	open    func(context.Context, model.ReportDatasource, string) (*reportoracle.Adapter, error)
}

var selectOraclePools = selectOraclePool{entries: map[uint]*selectOraclePoolEntry{}, open: func(ctx context.Context, ds model.ReportDatasource, password string) (*reportoracle.Adapter, error) {
	return reportoracle.Open(ctx, oracleConfigFromDatasource(ds, password))
}}

func acquireSelectOracle(ctx context.Context, ds model.ReportDatasource, password string) (*reportoracle.Adapter, func(), error) {
	return selectOraclePools.acquire(ctx, ds, password)
}
func (p *selectOraclePool) acquire(ctx context.Context, ds model.ReportDatasource, password string) (*reportoracle.Adapter, func(), error) {
	version := fmt.Sprintf("%s:%s", ds.UpdatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z"), ds.CredentialKeyVersion)
	p.Lock()
	entry := p.entries[ds.ID]
	if entry != nil && entry.version == version {
		entry.readers++
		p.Unlock()
		return entry.adapter, p.release(entry), nil
	}
	p.Unlock()
	// Opening a slow datasource must not block unrelated datasource pools.
	adapter, e := p.open(ctx, ds, password)
	if e != nil {
		return nil, nil, e
	}
	p.Lock()
	entry = p.entries[ds.ID]
	toClose := []*reportoracle.Adapter{}
	if entry != nil && entry.version == version {
		toClose = append(toClose, adapter)
	} else if entry != nil && entry.updatedAt.After(ds.UpdatedAt) {
		entry = &selectOraclePoolEntry{adapter: adapter, version: version, retired: true, updatedAt: ds.UpdatedAt}
	} else {
		if entry != nil {
			entry.retired = true
			if entry.readers == 0 {
				toClose = append(toClose, entry.adapter)
			}
		}
		entry = &selectOraclePoolEntry{adapter: adapter, version: version, updatedAt: ds.UpdatedAt}
		p.entries[ds.ID] = entry
	}
	entry.readers++
	p.Unlock()
	for _, retired := range toClose {
		_ = retired.Close()
	}
	return entry.adapter, p.release(entry), nil
}
func (p *selectOraclePool) release(entry *selectOraclePoolEntry) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			p.Lock()
			entry.readers--
			closeNow := entry.retired && entry.readers == 0
			p.Unlock()
			if closeNow {
				_ = entry.adapter.Close()
			}
		})
	}
}
