import { type SyntheticEvent, useState } from 'react'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'

import { useLightbox } from '../../hooks/useLightbox'
import { Icon } from '../Icon'
import {
  type ClusterAssignRequest,
  type ClusterView,
  type ExampleFace,
  type RemoveFaceRequest,
} from '../../services/people'
import { EnlargeButton } from '../review/EnlargeButton'
import { ReviewLightbox } from '../review/ReviewLightbox'

import { FaceThumb } from './FaceThumb'

import './clusters.css'

/**
 * The size the overlay shows a clustered face at: `fit_*` (the whole frame),
 * never the square `tile_*` the strip's crops are cut from — a centre-cropped
 * square is precisely what „is this the same person?" cannot be answered from,
 * and the face rectangle is placed in coordinates of the full photo.
 */
const CLUSTER_LIGHTBOX_SIZE = 'fit_1280'

/** Props for {@link ClusterCard}. */
export interface ClusterCardProps {
  /** The cluster awaiting a name. */
  cluster: ClusterView
  /** True while an action on this cluster is in flight. */
  busy: boolean
  /** Names the whole cluster (by existing subject UID or free-text name). */
  onAssign: (req: ClusterAssignRequest) => void
  /** Detaches a stray face before naming. */
  onRemoveFace: (ref: RemoveFaceRequest) => void
}

/** A stable key for an example face within a cluster. */
function exampleKey(face: ExampleFace): string {
  return `${face.photo_uid}:${String(face.face_index)}`
}

/**
 * One reviewable face cluster: a representative face, a strip of samples (each
 * removable if it does not belong), an optional one-tap "name as the suggested
 * subject" action, and a free-text name field. Naming applies to every face in
 * the cluster at once — the fast path the People UI is built around.
 *
 * Every crop in the card enlarges: they are 48–72 px squares cut from a tile, and
 * the question they ask ("is this the same person as those?") is one nobody can
 * answer at that size. The overlay steps through this cluster's own faces and
 * carries the sample's ✕ in its footer, so the stray face can be detached from
 * the picture that revealed it.
 */
export function ClusterCard({ cluster, busy, onAssign, onRemoveFace }: ClusterCardProps) {
  const { t } = useTranslation()
  const [name, setName] = useState('')
  // The representative first, then the strip: the same order the card draws, so
  // ←/→ walks the cluster exactly as the eye does.
  const faces = [cluster.representative, ...cluster.examples]
  const lightbox = useLightbox(faces)
  const enlarged = lightbox.item

  function handleSubmit(event: SyntheticEvent) {
    event.preventDefault()
    const trimmed = name.trim()
    if (trimmed !== '') {
      onAssign({ subject_name: trimmed })
    }
  }

  return (
    <Card className="h-100">
      <Card.Body className="d-flex flex-column gap-2">
        {/* Both this row and the naming row below wrap, because the card is now a
            cell of a grid whose column count is the user's: at the three columns a
            phone is capped to there is no room for a 72px face *beside* its badge,
            and content that cannot wrap simply spills out of the card. */}
        <div className="d-flex flex-wrap align-items-center gap-2">
          <EnlargeButton
            onEnlarge={() => {
              lightbox.open(0)
            }}
            className="d-inline-block w-auto"
          >
            <FaceThumb
              photoUid={cluster.representative.photo_uid}
              bbox={cluster.representative.bbox}
              label={t('clusters.representative')}
              size={72}
            />
          </EnlargeButton>
          <Badge bg="secondary">{t('clusters.size', { count: cluster.size })}</Badge>
        </div>

        <div className="d-flex flex-wrap gap-1">
          {cluster.examples.map((face, index) => (
            <div key={exampleKey(face)} className="kk-cluster-sample">
              <EnlargeButton
                onEnlarge={() => {
                  // +1: the representative is the overlay's first face.
                  lightbox.open(index + 1)
                }}
                className="d-inline-block w-auto"
              >
                <FaceThumb
                  photoUid={face.photo_uid}
                  bbox={face.bbox}
                  label={t('clusters.sample')}
                  size={48}
                />
              </EnlargeButton>
              {/* Geometry lives in `clusters.css`, not inline: on a coarse pointer the
                  control drops out of the thumbnail corner into a full-width row under
                  the sample, which no single set of inline sizes can express. */}
              <Button
                variant="danger"
                size="sm"
                className="kk-cluster-sample__remove d-inline-flex align-items-center justify-content-center"
                title={t('clusters.removeFace')}
                disabled={busy}
                aria-label={t('clusters.removeFace')}
                onClick={() => {
                  onRemoveFace({ photo_uid: face.photo_uid, face_index: face.face_index })
                }}
              >
                ✕
              </Button>
            </div>
          ))}
        </div>

        {cluster.suggestion && (
          <Button
            variant="outline-primary"
            size="sm"
            disabled={busy}
            onClick={() => {
              const suggestion = cluster.suggestion
              if (suggestion) {
                onAssign({ subject_uid: suggestion.subject_uid })
              }
            }}
          >
            {t('clusters.nameAs', {
              name: cluster.suggestion.subject_name,
              confidence: Math.round(cluster.suggestion.confidence * 100),
            })}
          </Button>
        )}

        <Form onSubmit={handleSubmit} className="mt-auto">
          <Form.Label htmlFor={`cluster-name-${cluster.uid}`} className="small text-secondary mb-1">
            {t('clusters.nameLabel')}
          </Form.Label>
          <div className="d-flex flex-wrap gap-2">
            <Form.Control
              // `flex-basis: 0`, not the control's own `width: 100%`: a wrapping
              // row is laid out from each item's hypothetical size, so a
              // full-width input would push the submit onto its own line at every
              // width. At zero basis the two share the row and only a track too
              // narrow for the button's own word wraps them apart.
              className="flex-grow-1"
              style={{ flexBasis: 0, minWidth: 0 }}
              id={`cluster-name-${cluster.uid}`}
              type="text"
              value={name}
              placeholder={t('clusters.namePlaceholder')}
              disabled={busy}
              onChange={(event) => {
                setName(event.target.value)
              }}
            />
            {/* `text-break` so the label's longest word may break rather than set a
                floor under the card: at the three columns a phone is capped to, a
                track is narrower than „Pojmenovat" and an unbreakable button is
                the last thing that would push the page sideways. It changes
                nothing at any width where the word fits. */}
            <Button
              type="submit"
              variant="primary"
              className="text-break"
              disabled={busy || name.trim() === ''}
            >
              {t('clusters.name')}
            </Button>
          </div>
        </Form>
      </Card.Body>

      {enlarged !== null && (
        <ReviewLightbox
          stage={{
            photoUid: enlarged.photo_uid,
            // A clustered face carries no catalogue dimensions, so the stage has
            // no estimate to open on and simply waits for the loaded preview.
            fileWidth: 0,
            fileHeight: 0,
            orientation: 0,
            size: CLUSTER_LIGHTBOX_SIZE,
            bbox: enlarged.bbox,
            href: `/photos/${enlarged.photo_uid}`,
            alt: t('clusters.sample'),
          }}
          title={t('clusters.size', { count: cluster.size })}
          onClose={lightbox.close}
          onPrev={lightbox.prev}
          onNext={lightbox.next}
          hasPrev={lightbox.hasPrev}
          hasNext={lightbox.hasNext}
        >
          {/* The representative has no ✕ on the card either: removing the face the
              cluster is represented by is not one of the card's offers, and the
              overlay repeats what the card can do rather than inventing more. */}
          {lightbox.index > 0 && (
            <Button
              variant="outline-danger"
              disabled={busy}
              className="d-flex align-items-center gap-1"
              onClick={() => {
                onRemoveFace({ photo_uid: enlarged.photo_uid, face_index: enlarged.face_index })
                lightbox.close()
              }}
            >
              <Icon name="x-lg" />
              {t('clusters.removeFace')}
            </Button>
          )}
        </ReviewLightbox>
      )}
    </Card>
  )
}
