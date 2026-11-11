package etc

import _ "embed"

//go:embed oracle_kaiyu_region_stay.sql
var OracleKaiyuRegionStaySQL string

//go:embed oracle_kaiyu_region_sync.sql
var OracleKaiyuRegionSyncSQL string
