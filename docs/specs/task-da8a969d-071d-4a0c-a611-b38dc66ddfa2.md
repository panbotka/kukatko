# A video in the slideshow just sits there

The slideshow renders every slide as an image. It knows when a slide is a video — but only to
switch off the slow pan-and-zoom effect. The frame it shows is the poster; the clip never plays,
nothing indicates it is a clip, and after the ordinary interval the slideshow moves on. Running a
slideshow over a video-only selection is confirmed to show seven motionless pictures.

## Requirements

- A video slide plays. It starts muted, plays from the beginning, and the slideshow advances when
  the clip ends rather than when the photo interval elapses.
- A long clip must not hold the slideshow hostage: cap how long one video slide may run — around
  thirty seconds is the right order — and advance when the cap is reached even if the clip is
  still going. Make the cap a named constant with a comment explaining the tension it resolves,
  not a magic number.
- Muted is the right default and the only behaviour this task needs: a slideshow that suddenly
  makes noise is worse than a silent one, and browsers block unmuted autoplay anyway. If sound is
  offered at all it is off unless the viewer asks for it.
- Pause, resume and manual next/previous work on a video slide as they do on a photo: pausing the
  slideshow pauses the clip, resuming resumes it, and skipping ahead abandons it cleanly.
- A clip that cannot play in this browser must not stall the slideshow. If playback does not start
  within a short grace period, or errors, fall back to holding the poster for the ordinary photo
  interval and move on. A slideshow that stops dead on one bad file is the failure to avoid.
- The slide still shows what it is: the play mark and duration that the library tile already
  carries belong here too, at least before playback starts.
- Leaving the slideshow must stop playback and release the video element — no clip left playing in
  the background.

## Notes

The library grid already has the helpers for deciding a slide is a video and for formatting a
duration; reuse them rather than re-deriving. The player component in the viewer is built for the
full playback chrome and is probably too much for a slide — a bare muted video element is likely
the better fit, but judge that against what the streaming playback hook needs, since a clip with
streaming renditions should use them here too.

## Tests

A video slide advancing on clip end; the cap firing on a long clip; a failing clip falling back to
the poster and the ordinary interval; pause and resume propagating to the clip; leaving the
slideshow stopping playback. Fake timers here need the option that lets time advance for real, or
async queries hang.
