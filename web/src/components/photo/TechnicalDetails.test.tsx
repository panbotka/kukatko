import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type PhotoDetail } from '../../services/photos'

import { TechnicalDetails } from './TechnicalDetails'

function photo(overrides: Partial<PhotoDetail> = {}): PhotoDetail {
  return {
    uid: 'b',
    file_hash: 'b',
    file_name: 'b.jpg',
    file_size: 100,
    file_mime: 'image/jpeg',
    file_width: 4000,
    file_height: 3000,
    taken_at_source: 'exif',
    thumb_url: '/api/v1/photos/b/thumb/tile_500',
    download_url: '/api/v1/photos/b/download?original=true',
    title: 'Beach',
    description: '',
    notes: '',
    camera_make: 'Canon',
    camera_model: 'EOS R5',
    lens_model: 'RF 24-70',
    iso: 200,
    aperture: 2.8,
    exposure: '1/250',
    focal_length: 50,
    created_at: '2026-01-02T10:00:00Z',
    updated_at: '2026-01-02T10:00:00Z',
    files: [],
    albums: [],
    labels: [],
    ...overrides,
  }
}

/** A photo carrying every field the card can render. */
function fullPhoto(overrides: Partial<PhotoDetail> = {}): PhotoDetail {
  return photo({
    file_hash: 'a'.repeat(64),
    file_size: 3145728,
    file_orientation: 6,
    original_name: 'IMG_0042.HEIC',
    camera_serial: 'SN-12345',
    software: 'Lightroom 13',
    subject: 'Sunset over the sea',
    keywords: 'beach, sunset',
    artist: 'Jan Novák',
    copyright: '© 2026 Jan Novák',
    license: 'CC BY-SA 4.0',
    color_profile: 'Display P3',
    image_codec: 'jpeg',
    projection: 'equirectangular',
    scan: true,
    private: true,
    lat: 49.19422,
    lng: 16.59922,
    altitude: 287.4,
    place: { country: 'Česko', region: 'Jihomoravský kraj', city: 'Brno', place_name: 'Špilberk' },
    photoprism_uid: 'pp-123',
    photosorter_uid: 'ps-456',
    uploader: { uid: 'u1', name: 'Camera Man' },
    ...overrides,
  })
}

function renderDetails(overrides: Partial<PhotoDetail> = {}, canWrite = false) {
  return render(
    <I18nextProvider i18n={i18n}>
      <TechnicalDetails
        photo={photo(overrides)}
        canWrite={canWrite}
        onThumbnailRegenerated={vi.fn()}
      />
    </I18nextProvider>,
  )
}

/** Expands the card and returns the user-event session that opened it. */
async function expand() {
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Technical details' }))
  return user
}

/** Renders a photo carrying every field, expanded. */
function renderFull(overrides: Partial<PhotoDetail> = {}) {
  return render(
    <I18nextProvider i18n={i18n}>
      <TechnicalDetails
        photo={fullPhoto(overrides)}
        canWrite={false}
        onThumbnailRegenerated={vi.fn()}
      />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('TechnicalDetails', () => {
  it('is collapsed on first render', () => {
    renderDetails()

    expect(screen.getByRole('button', { name: 'Technical details' })).toHaveAttribute(
      'aria-expanded',
      'false',
    )
    expect(screen.queryByText('EOS R5')).not.toBeInTheDocument()
    expect(screen.queryByText('ISO 200')).not.toBeInTheDocument()
    expect(screen.queryByText('b.jpg')).not.toBeInTheDocument()
    expect(screen.queryByText('File')).not.toBeInTheDocument()
  })

  it('reveals the camera, lens, EXIF, file name and dimensions when expanded', async () => {
    const user = userEvent.setup()
    renderDetails()

    await user.click(screen.getByRole('button', { name: 'Technical details' }))

    expect(screen.getByRole('button', { name: 'Technical details' })).toHaveAttribute(
      'aria-expanded',
      'true',
    )
    expect(screen.getByText('EOS R5')).toBeInTheDocument()
    expect(screen.getByText('RF 24-70')).toBeInTheDocument()
    expect(screen.getByText('f/2.8')).toBeInTheDocument()
    expect(screen.getByText('1/250 s')).toBeInTheDocument()
    expect(screen.getByText('50 mm')).toBeInTheDocument()
    expect(screen.getByText('ISO 200')).toBeInTheDocument()
    expect(screen.getByText('b.jpg')).toBeInTheDocument()
    expect(screen.getByText('4000 × 3000 px')).toBeInTheDocument()
  })

  it('collapses again on a second click', async () => {
    const user = userEvent.setup()
    renderDetails()

    const toggle = screen.getByRole('button', { name: 'Technical details' })
    await user.click(toggle)
    await user.click(toggle)

    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('EOS R5')).not.toBeInTheDocument()
  })

  it('states the upload as one fact — who, and when', async () => {
    renderDetails({ uploader: { uid: 'u1', name: 'Camera Man' } })

    // The upload is an intrinsic reference fact, so it lives here with the EXIF.
    expect(screen.queryByText('Upload')).not.toBeInTheDocument()
    await expand()
    const label = screen.getByText('Upload')
    expect(label.nextElementSibling).toHaveTextContent('Camera Man')
    // The moment it arrived belongs to the same sentence, not to another group.
    expect(label.nextElementSibling).toHaveTextContent(/2026/)
    expect(screen.queryByText('Added to the library')).not.toBeInTheDocument()
  })

  it('reads a photo with no uploader as imported, not as a dash', async () => {
    renderDetails()

    await expand()
    const label = screen.getByText('Upload')
    expect(label.nextElementSibling).toHaveTextContent(/Imported/)
    expect(label.nextElementSibling).not.toHaveTextContent('—')
  })

  it('shows the regenerate-thumbnail button to editors when expanded', async () => {
    const user = userEvent.setup()
    renderDetails({}, true)

    // Hidden until the section is expanded — it is a maintenance action, not a
    // primary control.
    expect(screen.queryByRole('button', { name: /regenerate thumbnail/i })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Technical details' }))
    expect(screen.getByRole('button', { name: /regenerate thumbnail/i })).toBeInTheDocument()
  })

  it('hides the regenerate-thumbnail button from viewers', async () => {
    const user = userEvent.setup()
    renderDetails({}, false)

    await user.click(screen.getByRole('button', { name: 'Technical details' }))
    expect(screen.queryByRole('button', { name: /regenerate thumbnail/i })).not.toBeInTheDocument()
  })

  it('omits rows a photo has no value for', async () => {
    renderDetails({
      camera_make: '',
      camera_model: '',
      lens_model: '',
      iso: undefined,
      aperture: undefined,
      exposure: '',
      focal_length: undefined,
      file_width: 0,
      file_height: 0,
    })

    await expand()

    expect(screen.queryByText('Camera')).not.toBeInTheDocument()
    expect(screen.queryByText('Lens')).not.toBeInTheDocument()
    expect(screen.queryByText('Dimensions')).not.toBeInTheDocument()
    expect(screen.queryByText('Aspect ratio')).not.toBeInTheDocument()
    expect(screen.queryByText('Resolution')).not.toBeInTheDocument()
    // The file name always survives.
    expect(screen.getByText('b.jpg')).toBeInTheDocument()
  })
})

describe('TechnicalDetails groups', () => {
  it('renders every group and field of a full payload', async () => {
    renderFull()
    const user = await expand()

    for (const group of ['Photo', 'File', 'Location', 'Origin']) {
      expect(screen.getByRole('heading', { name: group })).toBeInTheDocument()
    }

    // Photo — capture settings plus the IPTC/XMP credit block.
    expect(screen.getByText('SN-12345')).toBeInTheDocument()
    expect(screen.getByText('Lightroom 13')).toBeInTheDocument()
    expect(screen.getByText('EXIF')).toBeInTheDocument()
    expect(screen.getByText('Sunset over the sea')).toBeInTheDocument()
    expect(screen.getByText('Jan Novák')).toBeInTheDocument()
    expect(screen.getByText('© 2026 Jan Novák')).toBeInTheDocument()
    expect(screen.getByText('CC BY-SA 4.0')).toBeInTheDocument()
    expect(screen.getByText('equirectangular')).toBeInTheDocument()
    // The keywords are one comma-separated column, rendered as separate chips.
    expect(screen.getByText('beach')).toBeInTheDocument()
    expect(screen.getByText('sunset')).toBeInTheDocument()
    // The private and scan flags are badges.
    expect(screen.getByText('Private')).toBeInTheDocument()
    expect(screen.getByText('Scan')).toBeInTheDocument()

    // File — the stored bytes and what the app derives from them.
    expect(screen.getByText('IMG_0042.HEIC')).toBeInTheDocument()
    expect(screen.getByText('JPEG')).toBeInTheDocument()
    expect(screen.getByText('3.0 MB')).toBeInTheDocument()
    expect(screen.getByText('4 : 3')).toBeInTheDocument()
    expect(screen.getByText('12.0 MP')).toBeInTheDocument()
    expect(screen.getByText('Rotated 90° right')).toBeInTheDocument()
    expect(screen.getByText('Display P3')).toBeInTheDocument()
    expect(screen.getByText('Last modified')).toBeInTheDocument()

    // Location — the coordinate and the cached place, no geocode in sight.
    expect(screen.getByText('49.19422, 16.59922')).toBeInTheDocument()
    expect(screen.getByText('287 m')).toBeInTheDocument()
    expect(screen.getByText('Česko')).toBeInTheDocument()
    expect(screen.getByText('Brno')).toBeInTheDocument()
    expect(screen.getByText('Špilberk')).toBeInTheDocument()

    // Origin — how the photo got here, as one sentence. The import UIDs are one
    // click further down, in the developer section.
    expect(screen.getByText('Upload').nextElementSibling).toHaveTextContent('Camera Man')
    await user.click(screen.getByRole('button', { name: 'For developers' }))
    expect(screen.getByText('pp-123')).toBeInTheDocument()
    expect(screen.getByText('ps-456')).toBeInTheDocument()
  })

  it('shows the exact byte count in the size tooltip', async () => {
    renderFull()
    await expand()

    expect(screen.getByText('Size').nextElementSibling).toHaveAttribute('title', '3,145,728 B')
  })

  it('states the format once when the codec repeats it', async () => {
    // "Format: JPEG" over "Image codec: jpeg" is the same fact twice.
    renderFull({ file_mime: 'image/jpeg', image_codec: 'jpeg' })
    await expand()

    expect(screen.queryByText('Image codec')).not.toBeInTheDocument()
    expect(screen.getByText('Format').nextElementSibling).toHaveTextContent('JPEG')
  })

  it('keeps a codec that is genuinely a second fact, in the same row', async () => {
    renderFull({ file_mime: 'image/heic', image_codec: 'hevc' })
    await expand()

    expect(screen.getByText('Format').nextElementSibling).toHaveTextContent('HEIC (HEVC)')
  })
})

describe('TechnicalDetails developer section', () => {
  it('hides the hash and the import UIDs behind a collapsed expander', async () => {
    renderFull()
    await expand()

    // The card is open, but the identifiers a person has no use for are not.
    const toggle = screen.getByRole('button', { name: 'For developers' })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('Hash (SHA256)')).not.toBeInTheDocument()
    expect(screen.queryByText('pp-123')).not.toBeInTheDocument()
    expect(screen.queryByText('ps-456')).not.toBeInTheDocument()
  })

  it('truncates the hash, keeps the full value in a tooltip and copies it', async () => {
    renderFull()
    const user = await expand()
    await user.click(screen.getByRole('button', { name: 'For developers' }))

    const hash = 'a'.repeat(64)
    expect(screen.getByText(`${'a'.repeat(12)}…`)).toBeInTheDocument()
    expect(screen.getByText('Hash (SHA256)').nextElementSibling).toHaveAttribute('title', hash)

    // The clipboard glyph beside it has a tooltip of its own — the field's title
    // is the hash, the button's is what pressing it does.
    const copy = screen.getByRole('button', { name: 'Copy the hash' })
    expect(copy).toHaveAttribute('title', 'Copy the hash')

    await user.click(copy)
    await expect(navigator.clipboard.readText()).resolves.toBe(hash)
  })

  it('closes again on a second click', async () => {
    renderFull()
    const user = await expand()

    const toggle = screen.getByRole('button', { name: 'For developers' })
    await user.click(toggle)
    expect(screen.getByText('pp-123')).toBeInTheDocument()
    await user.click(toggle)

    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('pp-123')).not.toBeInTheDocument()
  })

  it('is absent for a photo with nothing to put in it', async () => {
    renderDetails({ file_hash: '', photoprism_uid: undefined, photosorter_uid: undefined })
    await expand()

    expect(screen.queryByRole('button', { name: 'For developers' })).not.toBeInTheDocument()
  })

  it('names the section in Czech too', async () => {
    await i18n.changeLanguage('cs')
    renderFull()
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Technické údaje' }))

    expect(screen.getByRole('button', { name: 'Pro vývojáře' })).toBeInTheDocument()
  })
})

describe('TechnicalDetails empty values', () => {
  it('renders no empty groups or rows for a sparse photo', async () => {
    renderDetails({
      camera_make: '',
      camera_model: '',
      lens_model: '',
      iso: undefined,
      aperture: undefined,
      exposure: '',
      focal_length: undefined,
      taken_at_source: '',
    })

    await expand()

    // Nothing is known about the capture, and no coordinate was ever recorded, so
    // neither group is rendered — not even as an empty heading.
    expect(screen.queryByRole('heading', { name: 'Photo' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Location' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Video' })).not.toBeInTheDocument()
    // The file is always there.
    expect(screen.getByRole('heading', { name: 'File' })).toBeInTheDocument()
    expect(screen.queryByText('Keywords')).not.toBeInTheDocument()
    expect(screen.queryByText('Flags')).not.toBeInTheDocument()
    expect(screen.queryByText('Original name')).not.toBeInTheDocument()
    expect(screen.queryByText('Country')).not.toBeInTheDocument()
  })

  it('renders no video group for a still photo', async () => {
    renderDetails({ media_type: 'image' })

    await expand()

    expect(screen.queryByRole('heading', { name: 'Video' })).not.toBeInTheDocument()
    expect(screen.queryByText('Duration')).not.toBeInTheDocument()
    expect(screen.queryByText('Frame rate')).not.toBeInTheDocument()
  })

  it('renders the video group for a clip', async () => {
    renderDetails({
      media_type: 'video',
      file_mime: 'video/quicktime',
      duration_ms: 154000,
      video_codec: 'h264',
      audio_codec: 'aac',
      has_audio: true,
      fps: 29.97,
    })

    await expand()

    expect(screen.getByRole('heading', { name: 'Video' })).toBeInTheDocument()
    expect(screen.getByText('2:34')).toBeInTheDocument()
    expect(screen.getByText('h264')).toBeInTheDocument()
    expect(screen.getByText('aac')).toBeInTheDocument()
    expect(screen.getByText('Yes')).toBeInTheDocument()
    expect(screen.getByText('29.97 fps')).toBeInTheDocument()
    expect(screen.getByText('MOV')).toBeInTheDocument()
  })

  it('reports a silent clip as having no audio', async () => {
    renderDetails({ media_type: 'video', duration_ms: 5000, has_audio: false })

    await expand()

    expect(screen.getByText('Audio').nextElementSibling).toHaveTextContent('No')
  })

  it("renders no row at all for the importers' `Unknown`", async () => {
    // PhotoPrism stores the word itself for a scan that never had a camera, so
    // the value is not empty — and "Camera: Unknown" is an English word in the
    // middle of a Czech table saying exactly as much as no row would.
    renderDetails({ camera_make: 'Unknown', camera_model: 'Unknown', lens_model: 'Unknown' })

    await expand()

    expect(screen.queryByText('Unknown')).not.toBeInTheDocument()
    expect(screen.queryByText('Camera')).not.toBeInTheDocument()
    expect(screen.queryByText('Lens')).not.toBeInTheDocument()
  })

  it('falls back to the camera make when only the model is `Unknown`', async () => {
    renderDetails({ camera_make: 'Canon', camera_model: 'Unknown' })

    await expand()

    expect(screen.getByText('Canon')).toBeInTheDocument()
    expect(screen.queryByText('Unknown')).not.toBeInTheDocument()
  })

  it('drops the whole group when `Unknown` was all it had', async () => {
    // A heading over nothing is the same blank noise as an empty row.
    renderDetails({
      camera_make: 'Unknown',
      camera_model: 'Unknown',
      lens_model: 'Unknown',
      iso: undefined,
      aperture: undefined,
      exposure: undefined,
      focal_length: undefined,
      taken_at_source: '',
    })

    await expand()

    expect(screen.queryByRole('heading', { name: 'Photo' })).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'File' })).toBeInTheDocument()
  })

  it('names the model behind an automatic description, out of the description', async () => {
    renderDetails({ ai_note: 'Skupina mužů v krojích.\n\nAI_MODEL: gemini-2.5-flash' })

    await expand()

    expect(screen.getByText('AI model')).toBeInTheDocument()
    expect(screen.getByText('gemini-2.5-flash')).toBeInTheDocument()
    // The description itself is the caption panel's business, not this card's.
    expect(screen.queryByText(/Skupina mužů/)).not.toBeInTheDocument()
  })

  it('has no AI model row when the note names none', async () => {
    renderDetails({ ai_note: 'Skupina mužů v krojích.' })

    await expand()

    expect(screen.queryByText('AI model')).not.toBeInTheDocument()
  })

  it('computes the aspect ratio and megapixels of a widescreen photo', async () => {
    renderDetails({ file_width: 1920, file_height: 1080 })

    await expand()

    expect(screen.getByText('16 : 9')).toBeInTheDocument()
    expect(screen.getByText('2.1 MP')).toBeInTheDocument()
  })

  it('falls back to a decimal aspect ratio for an odd crop', async () => {
    renderDetails({ file_width: 1001, file_height: 667 })

    await expand()

    expect(screen.getByText('1.50 : 1')).toBeInTheDocument()
  })

  it('formats the numbers with the Czech decimal comma', async () => {
    await i18n.changeLanguage('cs')
    renderDetails({ file_width: 1001, file_height: 667, file_size: 3145728 })

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Technické údaje' }))

    expect(screen.getByText('1,50 : 1')).toBeInTheDocument()
    expect(screen.getByText('3,0 MB')).toBeInTheDocument()
    expect(screen.getByText('0,7 Mpx')).toBeInTheDocument()
  })
})
