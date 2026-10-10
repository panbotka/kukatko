import { cleanup, render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { type BatchMembershipState } from '../../hooks/useBatchMembership'
import { type QueueItemStatus, type UploadQueueItem } from '../../hooks/useUploadQueue'
import i18n from '../../i18n'
import { type BulkMembershipSummary, type BulkResult } from '../../services/bulk'

import { UploadStageDone } from './UploadStageDone'

/** A settled queue item over a file of the given name. */
function item(id: string, name: string, status: QueueItemStatus = 'created'): UploadQueueItem {
  return {
    id,
    file: new File(['data'], name, { type: '' }),
    status,
    progress: 1,
    photoUid: status === 'error' ? undefined : `ph-${id}`,
  }
}

/** The aggregate counts a batch of the given items settles at. */
function summaryOf(items: UploadQueueItem[]) {
  const count = (status: QueueItemStatus) => items.filter((i) => i.status === status).length
  return {
    total: items.length,
    queued: 0,
    uploading: 0,
    created: count('created'),
    duplicate: count('duplicate'),
    error: count('error'),
  }
}

/** An empty bulk result, for an assignment that has come back. */
function bulkResult(): BulkResult {
  return { results: [], counts: { total: 0, updated: 0, skipped: 0, errored: 0 } }
}

/** A membership answer over `total` photos of which `filed` are in an album or label. */
function membershipOf(
  total: number,
  filed: number,
  albums: string[] = [],
  labels: string[] = [],
  partial = false,
): BatchMembershipState {
  const summary: BulkMembershipSummary = {
    total,
    filed,
    albums: albums.map((title, i) => ({ uid: `al${i}`, title, photo_count: 1 })),
    labels: labels.map((name, i) => ({ uid: `lb${i}`, name, photo_count: 1 })),
  }
  return { status: 'ready', summary, partial }
}

/**
 * Renders the finished stage over a settled batch, in the given language. The
 * album/label catalogs are `ready` and empty: this stage's copy is about the
 * batch, not about the picker's options. Unless told otherwise, the membership
 * question has answered that none of the batch is filed anywhere.
 */
async function renderDone(
  language: 'cs' | 'en',
  items: UploadQueueItem[],
  organizeNames: string[] = [],
  membership: BatchMembershipState = membershipOf(items.length, 0),
) {
  // One test asserts the same sentence in both languages, so the previous
  // render is taken down first — otherwise the second `getByText` finds two.
  cleanup()
  // The language has to be in place before the render: switching it afterwards
  // leaves the assertions racing i18next's re-render.
  await i18n.changeLanguage(language)
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <UploadStageDone
          summary={summaryOf(items)}
          items={items}
          organize={{
            load: { status: 'ready', albums: [], labels: [] },
            albums: [],
            labels: [],
            onAlbums: vi.fn(),
            onLabels: vi.fn(),
            disabled: false,
            allowCreate: true,
          }}
          organizeNames={organizeNames}
          assign={
            organizeNames.length > 0 ? { status: 'done', result: bulkResult() } : { status: 'idle' }
          }
          membership={membership}
          onRetryFailed={vi.fn()}
          onRemove={vi.fn()}
          onRetry={vi.fn()}
          onRetryAssign={vi.fn()}
          onUploadMore={vi.fn()}
        />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

afterEach(async () => {
  await i18n.changeLanguage('cs')
})

/**
 * The closing sentence must say what actually went up. A library that now takes
 * video cannot congratulate someone on „nahrána 1 fotka" when what they sent was
 * a film — and the Czech has to be a sentence a person would write, not a count
 * glued to a genitive noun.
 */
describe('UploadStageDone — what was uploaded', () => {
  it('keeps the photo wording for a stills-only batch', async () => {
    const items = [item('1', 'a.jpg'), item('2', 'b.jpg'), item('3', 'c.jpg')]

    await renderDone('cs', items)
    expect(screen.getByText('Nahrány 3 fotky.')).toBeInTheDocument()

    await renderDone('en', items)
    expect(screen.getByText('3 photos uploaded.')).toBeInTheDocument()
  })

  it('calls a clips-only batch what it is', async () => {
    await renderDone('cs', [item('1', 'a.mp4')])
    expect(screen.getByText('Nahráno 1 video.')).toBeInTheDocument()

    await renderDone('cs', [item('1', 'a.mp4'), item('2', 'b.mov')])
    expect(screen.getByText('Nahrána 2 videa.')).toBeInTheDocument()

    await renderDone(
      'cs',
      [1, 2, 3, 4, 5].map((n) => item(String(n), `${String(n)}.mp4`)),
    )
    expect(screen.getByText('Nahráno 5 videí.')).toBeInTheDocument()

    await renderDone('en', [item('1', 'a.mp4')])
    expect(screen.getByText('1 video uploaded.')).toBeInTheDocument()

    await renderDone('en', [item('1', 'a.mp4'), item('2', 'b.mov')])
    expect(screen.getByText('2 videos uploaded.')).toBeInTheDocument()
  })

  it('counts both halves of a mixed batch separately', async () => {
    const items = [item('1', 'a.jpg'), item('2', 'b.jpg'), item('3', 'c.jpg'), item('4', 'd.mov')]

    await renderDone('cs', items)
    expect(screen.getByText('Nahráli jsme 3 fotky a 1 video.')).toBeInTheDocument()

    await renderDone('en', items)
    expect(screen.getByText('3 photos and 1 video uploaded.')).toBeInTheDocument()
  })

  it('says the same when the batch has been filed into an album', async () => {
    const stills = [item('1', 'a.jpg'), item('2', 'b.jpg')]
    const clips = [item('1', 'a.mp4'), item('2', 'b.mov')]
    const mixed = [item('1', 'a.jpg'), item('2', 'b.mp4')]

    await renderDone('cs', stills, ['Pouť 2026'])
    expect(screen.getByText('Nahrány 2 fotky, přidány do: Pouť 2026.')).toBeInTheDocument()

    await renderDone('cs', clips, ['Pouť 2026'])
    expect(screen.getByText('Nahrána 2 videa, přidána do: Pouť 2026.')).toBeInTheDocument()

    await renderDone('cs', mixed, ['Pouť 2026'])
    expect(
      screen.getByText('Nahráli jsme 1 fotku a 1 video a přidali je do: Pouť 2026.'),
    ).toBeInTheDocument()

    await renderDone('en', stills, ['Trip'])
    expect(screen.getByText('2 photos uploaded, added to Trip.')).toBeInTheDocument()

    await renderDone('en', clips, ['Trip'])
    expect(screen.getByText('2 videos uploaded, added to Trip.')).toBeInTheDocument()

    await renderDone('en', mixed, ['Trip'])
    expect(screen.getByText('1 photo and 1 video uploaded, added to Trip.')).toBeInTheDocument()
  })

  it('counts only what was uploaded, leaving duplicates and failures to their own lines', async () => {
    // One clip through, one clip already in the library, one still that failed:
    // the sentence claims the one video and nothing else, and the two counters
    // keep counting files whatever kind of file they were.
    const items = [item('1', 'a.mp4'), item('2', 'b.mov', 'duplicate'), item('3', 'c.jpg', 'error')]

    await renderDone('en', items)
    expect(screen.getByText('1 file did not upload.')).toBeInTheDocument()

    await renderDone('en', [item('1', 'a.mp4'), item('2', 'b.mov', 'duplicate')])
    expect(screen.getByText('1 video uploaded.')).toBeInTheDocument()
    expect(screen.getByText('1 file was already in your library.')).toBeInTheDocument()
  })
})

/**
 * The copy around the sentence stops assuming stills too: a batch holding a clip
 * is not told its *photos* are in no album. Where no count is involved, one
 * wording covering both is enough — and a stills-only batch keeps the wording it
 * has always had.
 */
describe('UploadStageDone — the copy around it', () => {
  it('keeps the photo wording around a stills-only batch', async () => {
    await renderDone('cs', [item('1', 'a.jpg')])
    expect(
      screen.getByText(
        'Tyhle fotky zatím nejsou v žádném albu ani pod štítkem. Vyberte je a přidáme je tam.',
      ),
    ).toBeInTheDocument()

    await renderDone('en', [item('1', 'a.jpg')], ['Trip'])
    expect(
      screen.getByText('What you choose here is added to every photo in this batch.'),
    ).toBeInTheDocument()
  })

  it('drops the stills wording once a clip is in the batch', async () => {
    await renderDone('cs', [item('1', 'a.jpg'), item('2', 'b.mp4')])
    expect(
      screen.getByText(
        'Tyhle soubory zatím nejsou v žádném albu ani pod štítkem. Vyberte je a přidáme je tam.',
      ),
    ).toBeInTheDocument()

    await renderDone('en', [item('1', 'a.mp4')], ['Trip'])
    expect(
      screen.getByText('What you choose here is added to everything in this batch.'),
    ).toBeInTheDocument()
  })

  it('follows a duplicate clip too, which is filed along with the rest', async () => {
    // Nothing new was uploaded, so the sentence is about duplicates — but the
    // picker below still applies to that clip, and its copy must know.
    await renderDone('en', [item('1', 'a.jpg'), item('2', 'b.mp4', 'duplicate')])
    expect(
      screen.getByText(
        'These files are not in any album or label yet. Choose one and we will add them.',
      ),
    ).toBeInTheDocument()
  })
})

/**
 * "Not in any album or label yet" is a claim about the photos, and a duplicate
 * may have been filed by its first upload. The stage makes the claim only when
 * the membership question answered "nowhere"; otherwise it asks neutrally and,
 * when it knows, says where they already are.
 */
describe('UploadStageDone — where the batch already is', () => {
  const duplicates = [item('1', 'a.jpg', 'duplicate'), item('2', 'b.jpg', 'duplicate')]

  it('names the album duplicates already sit in instead of calling them unfiled', async () => {
    await renderDone('cs', duplicates, [], membershipOf(2, 2, ['Panorama']))
    expect(screen.getByText('Všechny 2 soubory už v knihovně byly.')).toBeInTheDocument()
    expect(
      screen.getByText('Všechny už jsou v: Panorama. Chcete je přidat do alba nebo pod štítek?'),
    ).toBeInTheDocument()
    expect(screen.queryByText(/v žádném albu/)).not.toBeInTheDocument()

    await renderDone('en', duplicates, [], membershipOf(2, 2, ['Panorama']))
    expect(
      screen.getByText(
        'All of them are already in Panorama. Would you like to add them to an album or label?',
      ),
    ).toBeInTheDocument()
    expect(screen.queryByText(/not in any album/)).not.toBeInTheDocument()
  })

  it('says "some" when only part of the batch is filed, labels included', async () => {
    const mixed = [item('1', 'a.jpg'), item('2', 'b.jpg', 'duplicate')]
    await renderDone('cs', mixed, [], membershipOf(2, 1, ['Panorama'], ['moře']))
    expect(
      screen.getByText(
        'Některé už jsou v: Panorama, moře. Chcete je přidat do alba nebo pod štítek?',
      ),
    ).toBeInTheDocument()

    await renderDone('en', mixed, [], membershipOf(2, 1, ['Panorama'], ['sea']))
    expect(
      screen.getByText(
        'Some of them are already in Panorama, sea. Would you like to add them to an album or label?',
      ),
    ).toBeInTheDocument()
  })

  it('names at most three places and counts the rest', async () => {
    await renderDone('cs', duplicates, [], membershipOf(2, 2, ['A', 'B', 'C', 'D', 'E']))
    expect(
      screen.getByText(
        'Všechny už jsou v: A, B, C a 2 další. Chcete je přidat do alba nebo pod štítek?',
      ),
    ).toBeInTheDocument()

    await renderDone('en', duplicates, [], membershipOf(2, 2, ['A', 'B', 'C', 'D']))
    expect(
      screen.getByText(
        'All of them are already in A, B, C and 1 more. Would you like to add them to an album or label?',
      ),
    ).toBeInTheDocument()
  })

  it('still says "not in any album" when the answer is nowhere', async () => {
    await renderDone('cs', duplicates, [], membershipOf(2, 0))
    expect(
      screen.getByText(
        'Tyhle fotky zatím nejsou v žádném albu ani pod štítkem. Vyberte je a přidáme je tam.',
      ),
    ).toBeInTheDocument()

    await renderDone('en', duplicates, [], membershipOf(2, 0))
    expect(
      screen.getByText(
        'These photos are not in any album or label yet. Choose one and we will add them.',
      ),
    ).toBeInTheDocument()
  })

  it('asks neutrally until it knows, after a failed question, and over a partial answer', async () => {
    const states: BatchMembershipState[] = [
      { status: 'loading' },
      { status: 'error' },
      { status: 'idle' },
      membershipOf(2, 0, [], [], true),
    ]
    for (const membership of states) {
      await renderDone('cs', duplicates, [], membership)
      expect(screen.getByText('Chcete je přidat do alba nebo pod štítek?')).toBeInTheDocument()
      expect(screen.queryByText(/v žádném albu/)).not.toBeInTheDocument()

      await renderDone('en', duplicates, [], membership)
      expect(
        screen.getByText('Would you like to add them to an album or label?'),
      ).toBeInTheDocument()
      expect(screen.queryByText(/not in any album/)).not.toBeInTheDocument()
    }
  })

  it('keeps a single photo singular', async () => {
    await renderDone('cs', [item('1', 'a.jpg', 'duplicate')], [], membershipOf(1, 1, ['Panorama']))
    expect(
      screen.getByText('Už je v: Panorama. Chcete je přidat do alba nebo pod štítek?'),
    ).toBeInTheDocument()

    await renderDone('en', [item('1', 'a.jpg', 'duplicate')], [], membershipOf(1, 1, ['Panorama']))
    expect(
      screen.getByText(
        'It is already in Panorama. Would you like to add them to an album or label?',
      ),
    ).toBeInTheDocument()
  })
})
