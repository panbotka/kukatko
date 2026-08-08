import { type CSSProperties } from 'react'

/** Props for {@link Skeleton}. */
export interface SkeletonProps {
  /** CSS width (a number is taken as px). Omit to fill the container. */
  width?: number | string
  /** CSS height (a number is taken as px). */
  height?: number | string
  /** Render as a circle — an avatar placeholder. */
  circle?: boolean
  /** Corner-radius override; defaults to the control radius, ignored when `circle`. */
  radius?: string
  /** Extra utility classes (spacing, aspect ratio). */
  className?: string
  /** Inline style overrides (e.g. `aspectRatio`). */
  style?: CSSProperties
}

/**
 * One skeleton block — a shimmering placeholder shaped like a single piece of the
 * eventual content: a cover, a line of text, an avatar. Decorative
 * (`aria-hidden`): the surrounding loading container owns the `role="status"`
 * and the announced label, so a screen reader hears "loading" once, not once per
 * block. The shimmer and its reduced-motion fallback live in `.kk-skeleton`.
 */
export function Skeleton({
  width,
  height,
  circle = false,
  radius,
  className,
  style,
}: SkeletonProps) {
  return (
    <span
      aria-hidden="true"
      className={['kk-skeleton', 'd-block', className].filter(Boolean).join(' ')}
      style={{
        width,
        height,
        borderRadius: circle ? '50%' : radius,
        ...style,
      }}
    />
  )
}

/** Props for {@link TileGridSkeleton}. */
export interface TileGridSkeletonProps {
  /** Localized "loading…" label, announced once to assistive tech. */
  label: string
  /** How many placeholder cards to draw. */
  count?: number
  /** Minimum tile width in px — matches the real grid's `minmax` so columns line up. */
  minTile?: number
  /** Grid gap in px. */
  gap?: number
  /** Caption lines under each cover (albums show two, people one). */
  captionLines?: number
}

/**
 * A loading placeholder for a card grid (albums, people): the same responsive
 * `minmax` columns and gap as the real grid, filled with square cover blocks
 * each carrying a line or two of caption placeholder — so the page holds its
 * eventual shape while the data arrives instead of flashing a bare spinner.
 */
export function TileGridSkeleton({
  label,
  count = 12,
  minTile = 160,
  gap = 12,
  captionLines = 2,
}: TileGridSkeletonProps) {
  return (
    <div
      role="status"
      aria-busy="true"
      aria-label={label}
      style={{
        display: 'grid',
        gridTemplateColumns: `repeat(auto-fill, minmax(${String(minTile)}px, 1fr))`,
        gap: `${String(gap)}px`,
      }}
    >
      {Array.from({ length: count }, (_, card) => (
        <div key={card} aria-hidden="true">
          <Skeleton radius="var(--kk-radius-md)" style={{ aspectRatio: '1 / 1' }} />
          {Array.from({ length: captionLines }, (_, line) => (
            <Skeleton
              key={line}
              className="mt-1"
              height="0.7rem"
              width={line === 0 ? '75%' : '45%'}
              radius="var(--kk-radius-pill)"
            />
          ))}
        </div>
      ))}
      <span className="visually-hidden">{label}</span>
    </div>
  )
}

/** Props for {@link ListSkeleton}. */
export interface ListSkeletonProps {
  /** Localized "loading…" label, announced once to assistive tech. */
  label: string
  /** How many placeholder rows to draw. */
  count?: number
  /** Row height, matching the real row's resting height. */
  rowHeight?: string
}

/**
 * A loading placeholder for a stacked list of rows (activity, decisions,
 * leaderboard): evenly spaced full-width blocks the height of a real row, so a
 * list view keeps its rhythm while loading rather than collapsing to a centered
 * spinner.
 */
export function ListSkeleton({ label, count = 8, rowHeight = '3.25rem' }: ListSkeletonProps) {
  return (
    <div role="status" aria-busy="true" aria-label={label} className="d-flex flex-column gap-2">
      {Array.from({ length: count }, (_, row) => (
        <Skeleton key={row} height={rowHeight} radius="var(--kk-radius-md)" />
      ))}
      <span className="visually-hidden">{label}</span>
    </div>
  )
}

/**
 * The widths, in rem, the chip placeholders cycle through. A cloud of identical
 * pills reads as a progress bar rather than as labels; a fixed cycle of uneven
 * ones looks like words without making the placeholder random (and so unstable
 * between renders and untestable).
 */
const CHIP_WIDTHS = [6, 9, 5, 11, 7, 8, 6.5, 10]

/** Props for {@link ChipCloudSkeleton}. */
export interface ChipCloudSkeletonProps {
  /** Localized "loading…" label, announced once to assistive tech. */
  label: string
  /** How many placeholder chips to draw. */
  count?: number
}

/**
 * A loading placeholder for a wrapping cloud of pills (labels): pill-shaped
 * blocks of uneven width, wrapped and gapped exactly as the real chips are, so
 * the page holds its eventual shape instead of drawing a stack of full-width
 * rows the data will never fill.
 */
export function ChipCloudSkeleton({ label, count = 24 }: ChipCloudSkeletonProps) {
  return (
    <div role="status" aria-busy="true" aria-label={label} className="d-flex flex-wrap gap-2">
      {Array.from({ length: count }, (_, chip) => (
        <Skeleton
          key={chip}
          height="2.25rem"
          width={`${String(CHIP_WIDTHS[chip % CHIP_WIDTHS.length])}rem`}
          radius="var(--kk-radius-pill)"
        />
      ))}
      <span className="visually-hidden">{label}</span>
    </div>
  )
}
