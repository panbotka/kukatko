import { useContext } from 'react'

import { useAuth } from '../auth/AuthContext'
import { useProjectUnread } from '../notifications/NotificationInboxContext'

import { ProjectContext } from './ProjectContext'
import { isEnterable } from './paths'

/** What {@link useLeaveProject} reports. */
export interface LeaveProject {
  /**
   * Whether the shell offers "Switch library" at all: the account may enter
   * more than one project, or it is the system administrator (who enters every
   * project, and whose chooser is also the way to their administration).
   */
  offered: boolean
  /**
   * The unread notifications waiting in the enterable projects the reader is
   * not in — every enterable project on a page outside one. The bell speaks for
   * the current project only, so this is the news the reader would otherwise
   * not see until the chooser.
   */
  othersUnread: number
}

/**
 * Whether the reader can leave this project for another, and what waits there.
 *
 * Switching is leaving: the shell's account menu (and the phone drawer's
 * account section) carry one "Switch library" entry leading to the chooser at
 * `/projects`, the one place a project is picked. One enterable project is a
 * fact, not a choice — the chooser would list the very project you are in — so
 * the entry is offered only beside a second one, or to the system
 * administrator. Outside a provider (an isolated component test) nothing is
 * offered.
 */
export function useLeaveProject(): LeaveProject {
  const ctx = useContext(ProjectContext)
  const { isMaintainer } = useAuth()
  const unreadOf = useProjectUnread()
  if (ctx === null) {
    return { offered: false, othersUnread: 0 }
  }
  const enterable = ctx.projects.filter(isEnterable)
  // On an instance page the context's project is only the entry rule's
  // fallback, not one the reader is in: every project is "another" there.
  const current = ctx.inAddress ? (ctx.project?.uid ?? null) : null
  const othersUnread = enterable
    .filter((m) => m.uid !== current)
    .reduce((sum, m) => sum + unreadOf(m.uid), 0)
  return { offered: enterable.length > 1 || isMaintainer, othersUnread }
}
