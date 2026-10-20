package data_svc

import (
	"context"
	"fmt"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/model"
	"sync"
)

type selectOraclePoolEntry struct {
	adapter *reportoracle.Adapter
	version string
	readers int
	retired bool
}

var selectOraclePools = struct {
	sync.Mutex
	entries map[uint]*selectOraclePoolEntry
}{entries: map[uint]*selectOraclePoolEntry{}}

func acquireSelectOracle(ctx context.Context, ds model.ReportDatasource, password string) (*reportoracle.Adapter, func(), error) {
	version := fmt.Sprintf("%s:%s", ds.UpdatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z"), ds.CredentialKeyVersion)
	selectOraclePools.Lock()
	entry := selectOraclePools.entries[ds.ID]
	if entry == nil || entry.version != version {
		adapter, e := reportoracle.Open(ctx, oracleConfigFromDatasource(ds, password))
		if e != nil {
			selectOraclePools.Unlock()
			return nil, nil, e
		}
		if entry != nil {
			entry.retired = true
			if entry.readers == 0 {
				_ = entry.adapter.Close()
			}
		}
		entry = &selectOraclePoolEntry{adapter: adapter, version: version}
		selectOraclePools.entries[ds.ID] = entry
	}
	entry.readers++
	selectOraclePools.Unlock()
	release := func() {
		selectOraclePools.Lock()
		defer selectOraclePools.Unlock()
		entry.readers--
		if entry.retired && entry.readers == 0 {
			_ = entry.adapter.Close()
		}
	}
	return entry.adapter, release, nil
}
