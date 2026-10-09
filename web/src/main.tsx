import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import 'bootswatch/dist/superhero/bootstrap.min.css'
// The theme's typeface, from files embedded in the binary. Bootswatch fetched
// Lato from Google; `build/localFonts.ts` strips that remote `@import` and this
// stylesheet declares the same weights locally, so a page load stays offline.
import './styles/fonts.css'
// The app's single icon set. `Icon` renders its glyphs as `bi bi-<name>` classes,
// so the font must be loaded globally rather than per component.
import 'bootstrap-icons/font/bootstrap-icons.css'
// The palette — the five colours (and the semantic block) every other colour is
// derived from — then the design token layer that derives them: the `--kk-*`
// custom properties the polish layer and the components consume. Then the
// bridge re-pointing Superhero's baked literal colours at those tokens, and the
// polish layer last so it can specialise any component on top of that baseline.
import './styles/palette.css'
import './styles/tokens.css'
import './styles/bootstrapBridge.css'
import './styles/app.css'
// The grid ⇄ viewer morph's choreography. Last, so its view-transition rules sit
// above the polish layer; it is inert wherever the API is missing.
import './styles/viewTransition.css'

import './i18n'
import { App } from './App'

const rootElement = document.getElementById('root')
if (!rootElement) {
  throw new Error('root element #root not found')
}

createRoot(rootElement).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
