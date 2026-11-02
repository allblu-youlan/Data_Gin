import { formatShanghaiDate, formatShanghaiDateTime } from './dateTime.js'
import { Download } from 'lucide-react'
import { type KeyboardEvent, type PointerEvent, type ReactNode, useEffect, useId, useMemo, useRef, useState } from 'react'
import {
  mallWeatherChartPoints,
  mallWeatherChartScale,
  mallWeatherChartSegments,
  mallWeatherChartTimeDomain,
  mallWeatherChartValuesByTime,
  mallWeatherClampedChartIndex,
  mallWeatherMetric,
  mallWeatherNearestChartPoint,
} from './mallWeather'
import styles from './MallWeatherChart.module.css'

const chartHeight = 260
const plot = { left: 52, right: 18, top: 18, bottom: 38 }
const plotHeight = chartHeight - plot.top - plot.bottom

export type MallWeatherChartSeries = {
  id: string
  name: string
  color: string
  dash?: string
  data: Array<{ time: string; value?: number }>
}

type MallWeatherChartProps = {
  title: string
  detail: string
  unit: string
  icon?: ReactNode
  series: MallWeatherChartSeries[]
  onDownload: (series: MallWeatherChartSeries[]) => void
  downloadDisabled?: boolean
  floorZero?: boolean
}

export function MallWeatherChart({
  title,
  detail,
  unit,
  icon,
  series,
  onDownload,
  downloadDisabled = false,
  floorZero = false,
}: MallWeatherChartProps) {
  const tooltipID = useId()
  const visualRef = useRef<HTMLDivElement>(null)
  const [activeIndex, setActiveIndex] = useState<number | null>(null)
  const [chartWidth, setChartWidth] = useState(720)
  const plotWidth = chartWidth - plot.left - plot.right
  const { timeDomain, normalizedSeries, scale } = useMemo(() => {
    const domain = mallWeatherChartTimeDomain(series.map((item) => item.data))
    const normalized = series.map((item) => {
      const values = mallWeatherChartValuesByTime(item.data, domain)
      return { ...item, data: domain.map((time, index) => ({ time, value: values[index] })) }
    })
    return {
      timeDomain: domain,
      normalizedSeries: normalized,
      scale: mallWeatherChartScale(normalized.flatMap((item) => item.data.map((point) => point.value)), { floorZero, tickCount: 5 }),
    }
  }, [floorZero, series])
  const itemCount = timeDomain.length
  const plottedSeries = useMemo(() => normalizedSeries.map((item) => {
    const values = item.data.map((point) => point.value)
    return {
      ...item,
      points: scale ? mallWeatherChartPoints(values, plotWidth, plotHeight, scale) : [],
      segments: scale ? mallWeatherChartSegments(values, plotWidth, plotHeight, scale) : [],
    }
  }), [normalizedSeries, plotWidth, scale])
  const axisPoints = useMemo(() => mallWeatherChartPoints(
    Array.from({ length: itemCount }, () => 0),
    plotWidth,
    plotHeight,
    { minimum: 0, maximum: 1 },
  ), [itemCount, plotWidth])
  const selectedPosition = mallWeatherClampedChartIndex(activeIndex, itemCount) ?? 0
  const activeAxisPoint = activeIndex === null ? undefined : axisPoints[selectedPosition]
  const selectedTime = chartTimeLabel(timeDomain[selectedPosition] || '')
  const selectedValues = normalizedSeries
    .map((item) => ({ id: item.id, name: item.name, color: item.color, value: item.data[selectedPosition]?.value }))
  const selectedY = plottedSeries
    .flatMap((item) => item.points.filter((point) => point.index === selectedPosition).map((point) => point.y))
    .sort((a, b) => a - b)[0] ?? plotHeight / 2
  const xTickIndexes = sampleIndexes(itemCount, Math.max(1, Math.min(4, Math.floor(plotWidth / 160))))
  const hasData = Boolean(scale && plottedSeries.some((item) => item.points.length > 0))

  useEffect(() => {
    setActiveIndex((current) => mallWeatherClampedChartIndex(current, itemCount))
  }, [itemCount])

  useEffect(() => {
    const element = visualRef.current
    if (!element) return
    const updateWidth = (width: number) => {
      const nextWidth = Math.max(260, Math.round(width))
      setChartWidth((current) => current === nextWidth ? current : nextWidth)
    }
    updateWidth(element.clientWidth)
    if (typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver((entries) => updateWidth(entries[0]?.contentRect.width ?? element.clientWidth))
    observer.observe(element)
    return () => observer.disconnect()
  }, [hasData])

  const selectNearestPoint = (event: PointerEvent<SVGSVGElement>) => {
    const bounds = event.currentTarget.getBoundingClientRect()
    if (bounds.width <= 0) return
    const pointerX = (event.clientX - bounds.left) / bounds.width * chartWidth - plot.left
    const nearest = mallWeatherNearestChartPoint(axisPoints, Math.max(0, Math.min(plotWidth, pointerX)))
    if (nearest) setActiveIndex(nearest.index)
  }

  const selectPointByKeyboard = (event: KeyboardEvent<SVGSVGElement>) => {
    let nextPosition = selectedPosition
    if (event.key === 'ArrowLeft' || event.key === 'ArrowDown') nextPosition = Math.max(0, selectedPosition - 1)
    else if (event.key === 'ArrowRight' || event.key === 'ArrowUp') nextPosition = Math.min(itemCount - 1, selectedPosition + 1)
    else if (event.key === 'Home') nextPosition = 0
    else if (event.key === 'End') nextPosition = itemCount - 1
    else if (event.key === 'Escape') {
      setActiveIndex(null)
      return
    } else return
    event.preventDefault()
    setActiveIndex(nextPosition)
  }

  const tooltipHorizontal = activeAxisPoint && activeAxisPoint.x <= plotWidth * 0.15
    ? 'start'
    : activeAxisPoint && activeAxisPoint.x >= plotWidth * 0.85 ? 'end' : 'center'
  const tooltipVertical = selectedY < plotHeight * 0.35 ? 'below' : 'above'

  return (
    <article className={styles.panel}>
      <header className={styles.heading}>
        <div className={styles.title}>
          {icon && <span className={styles.icon}>{icon}</span>}
          <div><strong>{title}</strong><span>{detail} · {unit}</span></div>
        </div>
        <button
          type="button"
          className={styles.download}
          aria-label={`下载数据：${title}`}
          disabled={downloadDisabled}
          onClick={() => onDownload(series)}
        >
          <Download aria-hidden="true" />下载数据
        </button>
      </header>
      <div className={styles.legend} aria-label="曲线图例">
        {series.map((item) => (
          <span key={item.id}>
            <svg viewBox="0 0 20 6" aria-hidden="true">
              <line x1="0" y1="3" x2="20" y2="3" stroke={item.color} strokeWidth="2" strokeDasharray={item.dash} />
            </svg>
            {item.name}
          </span>
        ))}
      </div>
      {!hasData ? (
        <div className={styles.empty} role="status">
          <strong>暂无趋势数据</strong>
          <span>当前时间窗口没有可绘制的数据。</span>
        </div>
      ) : (
        <div className={styles.visual} ref={visualRef}>
          <svg
            viewBox={`0 0 ${chartWidth} ${chartHeight}`}
            role="slider"
            tabIndex={0}
            aria-label={`${title}时间点`}
            aria-orientation="horizontal"
            aria-valuemin={0}
            aria-valuemax={Math.max(itemCount - 1, 0)}
            aria-valuenow={selectedPosition}
            aria-valuetext={`${selectedTime || '时间未知'}，${selectedValues.map((item) => `${item.name} ${mallWeatherMetric(item.value, unit)}`).join('，') || '暂无数据'}`}
            style={{ aspectRatio: `${chartWidth} / ${chartHeight}` }}
            onFocus={() => setActiveIndex((current) => current ?? 0)}
            onBlur={() => setActiveIndex(null)}
            onKeyDown={selectPointByKeyboard}
            onPointerMove={selectNearestPoint}
            onPointerLeave={(event) => {
              if (!event.currentTarget.matches(':focus')) setActiveIndex(null)
            }}
          >
            <title>{title}</title>
            <desc>使用鼠标悬停或方向键查看各时间点，默认隐藏数据点以保持曲线清晰。</desc>
            <g transform={`translate(${plot.left} ${plot.top})`}>
              {scale?.ticks.map((tick) => {
                const y = plotHeight - (tick - scale.minimum) / (scale.maximum - scale.minimum) * plotHeight
                return <g className={styles.yTick} key={tick}>
                  <line x1="0" x2={plotWidth} y1={y} y2={y} />
                  <text x="-10" y={y + 4} textAnchor="end">{chartAxisLabel(tick)}</text>
                </g>
              })}
              {xTickIndexes.map((index) => {
                const x = itemCount <= 1 ? 0 : index / (itemCount - 1) * plotWidth
                return <text className={styles.xTick} x={x} y={plotHeight + 25} textAnchor={index === 0 ? 'start' : index === itemCount - 1 ? 'end' : 'middle'} key={index}>
                  {chartTimeLabel(timeDomain[index] || '')}
                </text>
              })}
              {plottedSeries.map((item) => (
                <g className={styles.series} key={item.id}>
                  {item.segments.map((segment, index) => segment.includes(' ')
                    ? <polyline points={segment} style={{ stroke: item.color, strokeDasharray: item.dash }} key={index} />
                    : <ChartPoint point={segment} color={item.color} key={index} />)}
                </g>
              ))}
              {activeAxisPoint && (
                <g className={styles.active} aria-hidden="true">
                  <line x1={activeAxisPoint.x} x2={activeAxisPoint.x} y1="0" y2={plotHeight} />
                  {plottedSeries.map((item) => {
                    const point = item.points.find((candidate) => candidate.index === selectedPosition)
                    return point ? <circle cx={point.x} cy={point.y} r="4" style={{ stroke: item.color }} key={item.id} /> : null
                  })}
                </g>
              )}
            </g>
          </svg>
          {activeAxisPoint && (
            <div
              className={`${styles.tooltip} ${tooltipHorizontal === 'start' ? styles.tooltipStart : tooltipHorizontal === 'end' ? styles.tooltipEnd : styles.tooltipCenter} ${tooltipVertical === 'below' ? styles.tooltipBelow : ''}`}
              id={tooltipID}
              role="tooltip"
              style={{
                left: `${(plot.left + activeAxisPoint.x) / chartWidth * 100}%`,
                top: `${(plot.top + selectedY) / chartHeight * 100}%`,
              }}
            >
              <strong>{selectedTime || '时间未知'}</strong>
              {selectedValues.map((item) => <span key={item.id}><i style={{ backgroundColor: item.color }} />{item.name} {mallWeatherMetric(item.value, unit)}</span>)}
            </div>
          )}
        </div>
      )}
    </article>
  )
}

function ChartPoint({ point, color }: { point: string; color: string }) {
  const [cx = '0', cy = '0'] = point.split(',')
  return <circle className={styles.singlePoint} cx={cx} cy={cy} r="3" style={{ stroke: color }} />
}

function sampleIndexes(length: number, maximum: number) {
  if (length <= 0) return []
  if (maximum === 1) return [0]
  if (length <= maximum) return Array.from({ length }, (_, index) => index)
  const indexes = Array.from({ length: maximum }, (_, index) => Math.round(index * (length - 1) / (maximum - 1)))
  return [...new Set(indexes)]
}

function chartTimeLabel(value: string) {
  return /^\d{4}-\d{2}-\d{2}$/.test(value) ? formatShanghaiDate(value) : formatShanghaiDateTime(value, '')
}

function chartAxisLabel(value: number) {
  if (Math.abs(value) >= 1000) return Intl.NumberFormat('zh-CN', { notation: 'compact', maximumFractionDigits: 1 }).format(value)
  if (Number.isInteger(value)) return String(value)
  return value.toFixed(Math.abs(value) < 1 ? 2 : 1).replace(/0+$/, '').replace(/\.$/, '')
}
