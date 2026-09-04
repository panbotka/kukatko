# A photo viewer whose controls have vanished looks broken

On a phone the viewer's chrome fades out after about 2.6 seconds of no input, leaving only the
back arrow and a dimmed title. A real tap on the picture brings it back — verified on the live
instance — but nothing on screen says so. Someone who does not know the gesture sees a
photograph with no controls and concludes the screen is broken.

## Requirements

- Make it discoverable on a touch device that the controls come back: for example keep them up
  longer the first time a photograph is opened on that device, or show a brief one-time hint.
  A first-time reader must not be left with a photograph and no visible way to act on it.
- Keep the auto-hide. When the reader is just looking, the photograph should still own the
  screen; the goal is discoverability, not a permanently visible bar.
- Whatever is added must not fire on every photograph forever — a hint that reappears on the
  fiftieth photo is worse than no hint.

## Second half: check before changing

The bottom dock is anchored to the bottom edge and its last row of buttons ends exactly at the
viewport bottom. It already pads itself with `env(safe-area-inset-bottom)` and the app opts into
`viewport-fit=cover`, which is the standard handling, and **no occlusion could be reproduced**
in emulation at 390 x 844, at 360 x 640, or on an emulated iPhone 16. A user report of the
controls being "somehow fallen at the bottom" on a real phone is unconfirmed.

So: verify first, change second. Establish whether the dock is genuinely occluded or hard to
reach on a real phone browser whose own bar sits at the bottom, or whose visible viewport is
shorter than the layout viewport. If it is, fix it and say what the evidence was. If the
existing safe-area handling already covers it, say so and change nothing — do not add
speculative padding that pushes the controls up on every phone to solve a problem that may not
exist.

## Edge cases

- A browser whose bottom bar appears and disappears as the reader scrolls or taps.
- Landscape on a phone, where vertical space is scarce.
- The information sheet open, where the dock stands down and must not leave a gap behind.