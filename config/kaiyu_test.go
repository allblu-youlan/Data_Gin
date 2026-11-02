package config

import (
 "testing"
 "time"
 "github.com/robfig/cron/v3"
)

func TestKaiyuScheduleRunsOnceAnHourAtMinuteFive(t *testing.T){
 parser:=cron.NewParser(cron.Second|cron.Minute|cron.Hour|cron.Dom|cron.Month|cron.Dow)
 schedule,err:=parser.Parse(KaiyuHourlyCron);if err!=nil{t.Fatal(err)}
 start:=time.Date(2026,10,28,9,5,0,0,time.FixedZone("CST",8*3600))
 if next:=schedule.Next(start);!next.Equal(start.Add(time.Hour)){t.Fatal(next)}
}

func TestKaiyuCanBeDisabledAndRejectsInvalidSwitch(t *testing.T){
 t.Setenv("KAIYU_ENABLED","false")
 cfg,err:=LoadKaiyuConfig();if err!=nil||cfg.Enabled{t.Fatalf("cfg=%+v err=%v",cfg,err)}
 t.Setenv("KAIYU_ENABLED","invalid")
 if _,err:=LoadKaiyuConfig();err==nil{t.Fatal("invalid enable flag accepted")}
}
