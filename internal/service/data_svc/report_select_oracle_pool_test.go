package data_svc

import (
	"context"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/model"
	"sync/atomic"
	"testing"
	"time"
)

func TestSelectOraclePoolReusesConnectionsAndRotatesWithoutClosingActiveReaders(t *testing.T) {
	var opens atomic.Int32
	p := selectOraclePool{entries: map[uint]*selectOraclePoolEntry{}, open: func(context.Context, model.ReportDatasource, string) (*reportoracle.Adapter, error) {
		opens.Add(1)
		return &reportoracle.Adapter{}, nil
	}}
	ds := model.ReportDatasource{BaseModel: model.BaseModel{ID: 1}, CredentialKeyVersion: "v1"}
	first, release, e := p.acquire(t.Context(), ds, "secret")
	if e != nil {
		t.Fatal(e)
	}
	same, releaseSame, e := p.acquire(t.Context(), ds, "secret")
	if e != nil || same != first || opens.Load() != 1 {
		t.Fatal("pool was not reused")
	}
	old := p.entries[1]
	ds.CredentialKeyVersion = "v2"
	next, releaseNext, e := p.acquire(t.Context(), ds, "rotated")
	if e != nil || next == first || !old.retired || old.readers != 2 {
		t.Fatal("rotation did not preserve active readers")
	}
	release()
	release()
	releaseSame()
	releaseNext()
	if old.readers != 0 {
		t.Fatal("retired readers leaked")
	}
}
func TestSlowOracleOpenDoesNotBlockOtherDatasource(t *testing.T) {
	started, unblock := make(chan struct{}), make(chan struct{})
	p := selectOraclePool{entries: map[uint]*selectOraclePoolEntry{}, open: func(_ context.Context, ds model.ReportDatasource, _ string) (*reportoracle.Adapter, error) {
		if ds.ID == 1 {
			close(started)
			<-unblock
		}
		return &reportoracle.Adapter{}, nil
	}}
	firstDone := make(chan struct{})
	go func() {
		_, release, _ := p.acquire(t.Context(), model.ReportDatasource{BaseModel: model.BaseModel{ID: 1}}, "")
		release()
		close(firstDone)
	}()
	<-started
	otherDone := make(chan struct{})
	go func() {
		_, release, _ := p.acquire(t.Context(), model.ReportDatasource{BaseModel: model.BaseModel{ID: 2}}, "")
		release()
		close(otherDone)
	}()
	select {
	case <-otherDone:
	case <-time.After(time.Second):
		close(unblock)
		<-firstDone
		t.Fatal("slow source blocked another datasource")
	}
	close(unblock)
	<-firstDone
}
