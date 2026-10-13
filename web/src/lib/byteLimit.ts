import type { TFunction } from 'i18next'

import { formatBytes } from './format'

/**
 * A project limit (largest file, total quota) as the system administrator types
 * it: an amount and a unit, empty meaning "no limit". The units are binary, the
 * same 1024 steps `formatBytes` (`lib/format`) prints sizes with, so a
 * limit typed as "2 GB" reads back as "2.0 GB" on the projects table.
 */

/** The units a limit is typed in. */
export type LimitUnit = 'MB' | 'GB'

/** The units offered, smallest first. */
export const LIMIT_UNITS: readonly LimitUnit[] = ['MB', 'GB']

/** Bytes per unit. */
const UNIT_BYTES: Record<LimitUnit, number> = {
  MB: 1024 * 1024,
  GB: 1024 * 1024 * 1024,
}

/** A limit as the form holds it. */
export interface LimitDraft {
  /** The typed amount; `""` for no limit. */
  amount: string
  unit: LimitUnit
}

/** The outcome of reading a {@link LimitDraft}. */
export type ParsedLimit = { ok: true; bytes: number | null } | { ok: false }

/**
 * The draft a stored limit is edited from: whole gigabytes in GB, anything else
 * in MB (with up to two decimals), and an empty MB field for no limit.
 */
export function limitDraft(bytes: number | null): LimitDraft {
  if (bytes === null) {
    return { amount: '', unit: 'MB' }
  }
  if (bytes >= UNIT_BYTES.GB && bytes % UNIT_BYTES.GB === 0) {
    return { amount: String(bytes / UNIT_BYTES.GB), unit: 'GB' }
  }
  return { amount: String(Math.round((bytes / UNIT_BYTES.MB) * 100) / 100), unit: 'MB' }
}

/**
 * Reads a draft into bytes: `null` for an empty amount (no limit), a refusal for
 * anything that is not a positive number. A decimal comma is accepted, since
 * that is how Czech writes "1,5".
 */
export function parseLimit(draft: LimitDraft): ParsedLimit {
  const text = draft.amount.trim().replace(',', '.')
  if (text === '') {
    return { ok: true, bytes: null }
  }
  if (!/^\d+(\.\d+)?$/.test(text)) {
    return { ok: false }
  }
  const bytes = Math.round(Number(text) * UNIT_BYTES[draft.unit])
  if (!Number.isFinite(bytes) || bytes < 1 || !Number.isSafeInteger(bytes)) {
    return { ok: false }
  }
  return { ok: true, bytes }
}

/** The two limits of a project, as the wire carries them (bytes or null). */
export interface ProjectLimits {
  max_file_bytes: number | null
  max_total_bytes: number | null
}

/**
 * A project's limits in one line: each one in readable bytes (or „neomezeno"),
 * or a single „neomezeno" when neither is set.
 */
export function formatLimits(limits: ProjectLimits, t: TFunction, locale: string): string {
  if (limits.max_file_bytes === null && limits.max_total_bytes === null) {
    return t('systemProjects.unlimited')
  }
  const limit = (bytes: number | null) =>
    bytes === null ? t('systemProjects.unlimited') : formatBytes(bytes, locale)
  return [
    t('systemProjects.limitFile', { value: limit(limits.max_file_bytes) }),
    t('systemProjects.limitTotal', { value: limit(limits.max_total_bytes) }),
  ].join(' · ')
}
