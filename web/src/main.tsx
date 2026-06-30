import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import 'bootswatch/dist/superhero/bootstrap.min.css'
import './styles/app.css'

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
