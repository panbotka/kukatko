# Keyboard control of the video player

Playing a video is nearly mouse-only. The player binds four keys — play/pause, ten seconds back
and forward, and speed — and the most obvious key of all, the space bar, does nothing. Everything
else a person expects from a video player is taken by the surrounding viewer: the arrows page to
the previous and next media item, one letter toggles the favourite, another toggles the face
overlay, and the digits set the star rating.

Give the player the full set people already know, resolved by scope rather than by giving up
keys: **while the video is playing, or the player has focus, the player owns those keys; at every
other moment they keep the meaning they have today.**

## How the keys already work here

- There is a shared keyboard hook that owns a single document listener, suppresses shortcuts
  while typing and inside form dialogs, and lets a focused button or link consume the activation
  keys before a shortcut sees them.
- The player registers its own map through that hook, already scoped to "focused or playing".
- The star rating is the exception: it is handled by a **second, separate document listener** on
  the detail page that does not consult the shared map at all. This is the trap in this task.
- The help overlay behind `?` is a data-only registry listing the shortcuts per context,
  including a video group. It documents; it does not dispatch. Both places need editing.

## Requirements

### While the player is focused or the video is playing

- Space and `k` — play/pause.
- Left and right arrow — seek five seconds back and forward.
- `j` and `l` — seek ten seconds back and forward.
- `0` to `9` — jump to that tenth of the clip (`0` restarts it).
- `m` — mute and unmute.
- `f` — enter and leave fullscreen.
- `<` and `>` — step the playback speed down and up, as they do today.
- `,` and `.` — while paused, nudge the position by roughly a single frame back and forward.
- Escape leaves fullscreen when fullscreen is active, and does **not** close the viewer in that
  case. Outside fullscreen it keeps today's behaviour.

### At every other moment

Every one of those keys keeps its current meaning: the arrows page between media items, `f`
toggles the favourite, `m` toggles the face overlay, the digits set the star rating, and so on.
Nothing at all changes for a still photo.

### The two traps

- **Space double-firing.** Every control in the player chrome is a real button, and the shared
  hook deliberately yields the activation keys to a focused button. After the user clicks play,
  the play button holds focus, so a space press would re-activate that button rather than reach a
  shortcut. The player has to handle the space bar itself in that state and suppress the default
  activation, so one press produces one toggle — never two, and never a scroll.
- **Digits firing twice.** The rating listener is a separate listener that knows nothing about the
  player's scope. Without changing it, pressing `4` on a playing video would both seek to 40% and
  award four stars. Teach it the same scope, so exactly one of the two acts.

### The help overlay

Rewrite the video group of the shortcut registry to the new set, and state the scope rule in it —
that these apply while the video is playing or the player is focused, and that the same keys mean
something else otherwise. Czech default, English second, both filled.

## Tests

Cover the precedence in both directions: each key acting on the player while it is playing or
focused, and the same key acting on the page while it is not. Cover a single space press
producing a single toggle with the play button focused, and a digit press producing a seek and no
rating change while playing, and a rating change and no seek while not.
