import './charts.css'

/**
 * What a chart card holds when its series has no chart in it: the one number the
 * panel would have drawn, and the sentence that says why there is nothing to
 * compare it with.
 *
 * A series with a single filled bucket is not a comparison, and drawing it as one
 * misleads twice over — a lone full-height bar reads as a rendering fault rather
 * than as a measurement, and eleven zeroes beside one bar spend a whole card
 * saying "nothing happened" eleven times. The number is the same number the chart
 * was scaled to, so nothing is lost by not drawing it; the note names the bucket
 * it fell in, which the bar alone never said out loud.
 *
 * It is plain text, so it needs no `aria-label`: a screen reader gets the value
 * and the sentence in the order they are read on screen.
 */
export function ChartStatement({
  value,
  note,
  testId,
}: {
  value: string
  note: string
  testId?: string
}) {
  return (
    <div className="kk-chart-statement" data-testid={testId}>
      {/* Proportional figures, not the tabular ones of a column of numbers: this
          is one standalone figure and `tabular-nums` reads loose at this size. */}
      <p className="kk-display mb-0">{value}</p>
      <p className="text-secondary kk-text-caption mb-0">{note}</p>
    </div>
  )
}
