import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import 'bootswatch/dist/superhero/bootstrap.min.css'
// The app's single icon set. `Icon` renders its glyphs as `bi bi-<name>` classes,
// so the font must be loaded globally rather than per component.
import 'bootstrap-icons/font/bootstrap-icons.css'
// The design token layer sits between Bootswatch and the polish layer: it
// defines the `--kk-*` custom properties that `app.css` and the components
// consume, so it must be imported before them.
import './styles/tokens.css'
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
