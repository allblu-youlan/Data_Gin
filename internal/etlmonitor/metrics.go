package etlmonitor

import (
	"encoding/json"
	"errors"
	"io"
	"maps"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

func parseMetrics(reader io.Reader) ([]Series, error) {
	var parser expfmt.TextParser
	families, err := parser.TextToMetricFamilies(reader)
	if err != nil {
		return nil, err
	}
	series := []Series{}
	for name, family := range families {
		kind := strings.ToLower(family.GetType().String())
		for _, metric := range family.Metric {
			labels := map[string]string{}
			for _, label := range metric.Label {
				labels[label.GetName()] = label.GetValue()
			}
			add := func(name string, value float64, extraKey, extraValue string) {
				l := maps.Clone(labels)
				if extraKey != "" {
					l[extraKey] = extraValue
				}
				series = append(series, Series{Name: name, Type: kind, Labels: l, Value: finite(value)})
			}
			switch family.GetType() {
			case dto.MetricType_COUNTER:
				add(name, metric.GetCounter().GetValue(), "", "")
			case dto.MetricType_GAUGE:
				add(name, metric.GetGauge().GetValue(), "", "")
			case dto.MetricType_UNTYPED:
				add(name, metric.GetUntyped().GetValue(), "", "")
			case dto.MetricType_SUMMARY:
				s := metric.GetSummary()
				if s.SampleSum != nil {
					add(name+"_sum", s.GetSampleSum(), "", "")
				}
				if s.SampleCount != nil {
					add(name+"_count", float64(s.GetSampleCount()), "", "")
				}
				for _, q := range s.Quantile {
					add(name, q.GetValue(), "quantile", strconv.FormatFloat(q.GetQuantile(), 'g', -1, 64))
				}
			case dto.MetricType_HISTOGRAM:
				h := metric.GetHistogram()
				if h.SampleSum != nil {
					add(name+"_sum", h.GetSampleSum(), "", "")
				}
				if h.SampleCount != nil {
					add(name+"_count", float64(h.GetSampleCount()), "", "")
				}
				for _, bucket := range h.Bucket {
					add(name+"_bucket", float64(bucket.GetCumulativeCount()), "le", strconv.FormatFloat(bucket.GetUpperBound(), 'g', -1, 64))
				}
			}
		}
	}
	if len(series) == 0 {
		return nil, errors.New("no Prometheus metric samples")
	}
	sort.Slice(series, func(i, j int) bool { return seriesKey(series[i]) < seriesKey(series[j]) })
	return series, nil
}

func finite(v float64) *float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}

func seriesKey(s Series) string { labels, _ := json.Marshal(s.Labels); return s.Name + string(labels) }

func applyRates(current, previous []Series, previousAt *time.Time, now time.Time) {
	if previousAt == nil || !now.After(*previousAt) {
		return
	}
	prev := make(map[string]Series, len(previous))
	for _, s := range previous {
		prev[seriesKey(s)] = s
	}
	for _, s := range current {
		if s.Name == "process_start_time_seconds" {
			if old, ok := prev[seriesKey(s)]; ok && s.Value != nil && old.Value != nil && *s.Value != *old.Value {
				return
			}
		}
	}
	elapsed := now.Sub(*previousAt).Seconds()
	for i := range current {
		s := &current[i]
		monotonic := s.Type == "counter" || ((s.Type == "summary" || s.Type == "histogram") && (strings.HasSuffix(s.Name, "_sum") || strings.HasSuffix(s.Name, "_count") || strings.HasSuffix(s.Name, "_bucket")))
		if !monotonic || s.Value == nil {
			continue
		}
		old, ok := prev[seriesKey(*s)]
		if !ok || old.Value == nil || *s.Value < *old.Value {
			continue
		}
		s.RatePerSecond = finite((*s.Value - *old.Value) / elapsed)
	}
}

// Select one measurement per pipeline boundary when a broker and its children
// report the same metric. Raw series remain available for component diagnosis.
func selectSeries(series []Series, name string, boundary bool) []Series {
	matched := []Series{}
	for _, s := range series {
		if s.Name == name || s.Name == name+"_total" {
			matched = append(matched, s)
		}
	}
	if !boundary {
		return matched
	}
	result := []Series{}
	for _, s := range matched {
		child := false
		for _, parent := range matched {
			p := parent.Labels["path"]
			if p != "" && s.Labels["stream"] == parent.Labels["stream"] && strings.HasPrefix(s.Labels["path"], p+".") {
				child = true
				break
			}
		}
		if !child {
			result = append(result, s)
		}
	}
	return result
}

func total(series []Series, name string, rate, boundary bool) *float64 {
	matched := selectSeries(series, name, boundary)
	var result *float64
	for _, s := range matched {
		v := s.Value
		if rate {
			v = s.RatePerSecond
		}
		// A partial component rate would make the aggregate look like a real drop.
		if v == nil {
			return nil
		}
		if result == nil {
			result = finite(0)
		}
		*result += *v
	}
	if result == nil {
		return nil
	}
	return finite(*result)
}

func averageLatency(series []Series, name string, boundary bool) *float64 {
	sums := selectSeries(series, name+"_sum", boundary)
	counts := selectSeries(series, name+"_count", boundary)
	countByLabel := map[string]float64{}
	for _, s := range counts {
		if s.Value != nil {
			labels, _ := json.Marshal(s.Labels)
			countByLabel[string(labels)] = *s.Value
		}
	}
	var sum, count float64
	for _, s := range sums {
		if s.Value == nil {
			continue
		}
		labels, _ := json.Marshal(s.Labels)
		n, ok := countByLabel[string(labels)]
		if !ok {
			continue
		}
		// Bento retains the _ns name in histogram mode but exports seconds.
		factor := 1000.0
		if strings.HasSuffix(name, "_ns") && s.Type != "histogram" {
			factor = 1e-6
		}
		sum += *s.Value * factor
		count += n
	}
	if count == 0 {
		return nil
	}
	return finite(sum / count)
}

func summarize(series []Series, now time.Time) []Indicator {
	result := []Indicator{}
	add := func(key, label, unit, description string, value *float64) {
		result = append(result, Indicator{Key: key, Label: label, Unit: unit, Description: description, Value: value})
	}
	boundary := "按输入/输出最外层可见组件统计；消息可能包含一页业务数据，不等于订单数。"
	rateNote := "相邻成功采样的计数增量/秒；首次采样、重启或新增序列暂不可用。"
	add("input_received_total", "累计接收消息", "条", boundary, total(series, "input_received", false, true))
	add("input_rate", "输入吞吐", "条/秒", rateNote, total(series, "input_received", true, true))
	add("output_sent_total", "累计输出消息", "条", boundary, total(series, "output_sent", false, true))
	add("output_rate", "输出吞吐", "条/秒", rateNote, total(series, "output_sent", true, true))
	add("output_batch_total", "累计输出批次", "批", boundary, total(series, "output_batch_sent", false, true))
	add("processor_error_total", "处理器错误", "次", "所有处理阶段的报错次数，批量报错可仅计一次。", total(series, "processor_error", false, false))
	add("output_error_total", "输出失败尝试", "次", "发送失败次数；批量发送失败计一次。", total(series, "output_error", false, true))
	processorRate := total(series, "processor_error", true, false)
	outputRate := total(series, "output_error", true, true)
	var errorRate *float64
	if processorRate != nil && outputRate != nil {
		errorRate = finite(*processorRate + *outputRate)
	}
	add("error_rate", "处理及输出错误速率", "次/秒", "处理器和输出错误速率之和；两类序列都可用后显示。", errorRate)
	add("input_latency_ms", "输入平均往返耗时", "ms", "自进程启动的累计均值，包含下游确认或缓冲等待。", averageLatency(series, "input_latency_ns", true))
	add("processor_latency_ms", "处理器平均耗时", "ms", "各处理阶段累计总耗时/累计调用次数。", averageLatency(series, "processor_latency_ns", false))
	add("output_latency_ms", "输出平均耗时", "ms", "自进程启动的累计写入耗时/调用次数；非最近窗口分位数。", averageLatency(series, "output_latency_ns", true))
	for _, spec := range []struct {
		key, name, label string
		boundary         bool
	}{
		{"input_connection_failed_total", "input_connection_failed", "输入连接失败", true},
		{"input_connection_lost_total", "input_connection_lost", "输入连接丢失", true},
		{"output_connection_failed_total", "output_connection_failed", "输出连接失败", true},
		{"output_connection_lost_total", "output_connection_lost", "输出连接丢失", true},
		{"buffer_received_total", "buffer_received", "缓冲接收消息", true},
		{"buffer_sent_total", "buffer_sent", "缓冲发出消息", true},
		{"cache_error_total", "cache_error", "缓存错误", false},
		{"rate_limit_triggered_total", "rate_limit_triggered", "限流触发", false},
	} {
		add(spec.key, spec.label, "次", "实例启动后的组件累计值；缺少序列时显示未知。", total(series, spec.name, false, spec.boundary))
	}
	add("rss_bytes", "进程常驻内存", "bytes", "进程实际驻留内存，需要开启 add_process_metrics。", total(series, "process_resident_memory_bytes", false, false))
	add("heap_bytes", "Go 堆占用", "bytes", "当前分配的堆内存，需要开启 add_go_metrics。", total(series, "go_memstats_heap_alloc_bytes", false, false))
	add("goroutines", "Goroutine 数", "个", "当前 Go 协程数量。", total(series, "go_goroutines", false, false))
	add("gc_pause_ms", "平均 GC 暂停", "ms", "累计 GC 暂停时长/累计 GC 次数。", averageLatency(series, "go_gc_duration_seconds", false))
	cpu := total(series, "process_cpu_seconds_total", true, false)
	if cpu != nil {
		*cpu *= 100
	}
	add("cpu_percent", "进程 CPU 使用率", "%", "CPU 秒增量/墙钟秒×100，多核使用可能超过100%。", cpu)
	var uptime *float64
	if start := total(series, "process_start_time_seconds", false, false); start != nil && *start > 0 {
		uptime = finite(math.Max(0, float64(now.UnixNano())/1e9-*start))
	}
	add("uptime_seconds", "进程运行时长", "秒", "根据进程启动时间计算。", uptime)
	add("open_fds", "打开文件描述符", "个", "当前进程打开的文件描述符，需要开启 add_process_metrics。", total(series, "process_open_fds", false, false))
	return result
}
