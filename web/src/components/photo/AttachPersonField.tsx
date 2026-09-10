import { useTranslation } from 'react-i18next'

import { useSubjects } from '../../hooks/useSubjects'

import { AddAutocomplete } from './AddAutocomplete'

/** Props for {@link AttachPersonField}. */
export interface AttachPersonFieldProps {
  /**
   * The subjects already on the media item — offering them again would attach a
   * link that is already there. They are held out of the options and named to the
   * field, so typing one of them is not offered for creation either.
   */
  attached: readonly { uid: string; name: string }[]
  /** True while an attach is in flight; disables the field. */
  busy?: boolean
  /** Attaches the picked subject to the media item. */
  onPick: (subjectUid: string) => void
  /**
   * Creates a subject of that name and attaches it, resolving `true` once both
   * have happened (the field then clears) and `false` when either failed — the
   * typed text is kept so the reader can try again.
   */
  onCreate: (name: string) => Promise<boolean>
}

/**
 * The people picker of the photo panel: the same type-to-filter field the face
 * naming flow uses ({@link AddAutocomplete} over every subject in the library),
 * pointed at "who is in this picture" rather than at one detected face.
 *
 * It is a component of its own for one reason: it is what fetches the subject
 * list, and that list must not be fetched by every photo the reader opens. The
 * panel mounts this only once the reader has actually asked to add somebody, so
 * browsing costs nothing — the same bargain `FacesPanel` strikes by mounting only
 * when the faces view opens.
 *
 * A name nothing in the library carries is offered for creation, so somebody who
 * has never been named anywhere can still be recorded here; the new subject takes
 * the plain defaults the People page would give it, which stay editable there.
 */
export function AttachPersonField({
  attached,
  busy = false,
  onPick,
  onCreate,
}: AttachPersonFieldProps) {
  const { t } = useTranslation()
  const { subjects, loading } = useSubjects()

  const taken = new Set(attached.map((subject) => subject.uid))

  return (
    <AddAutocomplete
      id="organize-add-person"
      label={t('photo.organize.addPersonLabel')}
      autoFocus
      disabled={busy || loading}
      options={subjects
        .filter((subject) => !taken.has(subject.uid))
        .map((subject) => ({
          uid: subject.uid,
          label: subject.name,
          // "Who is in this picture" is a question about pictures, so the hint
          // counts photos rather than the faces the naming flow counts.
          hint: String(subject.photo_count),
        }))}
      existingNames={attached.map((subject) => subject.name)}
      onAdd={onPick}
      onCreate={onCreate}
    />
  )
}
