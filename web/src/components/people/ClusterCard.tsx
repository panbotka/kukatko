import { type SyntheticEvent, useState } from 'react'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'

import {
  type ClusterAssignRequest,
  type ClusterView,
  type ExampleFace,
  type RemoveFaceRequest,
} from '../../services/people'

import { FaceThumb } from './FaceThumb'

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
 */
export function ClusterCard({ cluster, busy, onAssign, onRemoveFace }: ClusterCardProps) {
  const { t } = useTranslation()
  const [name, setName] = useState('')

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
        <div className="d-flex align-items-center gap-2">
          <FaceThumb
            photoUid={cluster.representative.photo_uid}
            bbox={cluster.representative.bbox}
            label={t('clusters.representative')}
            size={72}
          />
          <Badge bg="secondary">{t('clusters.size', { count: cluster.size })}</Badge>
        </div>

        <div className="d-flex flex-wrap gap-1">
          {cluster.examples.map((face) => (
            <div key={exampleKey(face)} className="position-relative">
              <FaceThumb
                photoUid={face.photo_uid}
                bbox={face.bbox}
                label={t('clusters.sample')}
                size={48}
              />
              <Button
                variant="danger"
                size="sm"
                className="position-absolute top-0 end-0 p-0 lh-1"
                style={{ width: '18px', height: '18px', fontSize: 'var(--kk-font-size-caption)' }}
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
          <div className="d-flex gap-2">
            <Form.Control
              id={`cluster-name-${cluster.uid}`}
              type="text"
              value={name}
              placeholder={t('clusters.namePlaceholder')}
              disabled={busy}
              onChange={(event) => {
                setName(event.target.value)
              }}
            />
            <Button type="submit" variant="primary" disabled={busy || name.trim() === ''}>
              {t('clusters.name')}
            </Button>
          </div>
        </Form>
      </Card.Body>
    </Card>
  )
}
