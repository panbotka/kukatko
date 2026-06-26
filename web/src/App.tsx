import { BrowserRouter, Route, Routes } from 'react-router-dom'

import { AuthProvider } from './auth/AuthProvider'
import { RequireAuth, RequireRole } from './auth/ProtectedRoute'
import { Layout } from './components/Layout'
import { AccountPage } from './pages/AccountPage'
import { HomePage } from './pages/HomePage'
import { LibraryPage } from './pages/LibraryPage'
import { LoginPage } from './pages/LoginPage'
import { NotFoundPage } from './pages/NotFoundPage'
import { SearchPage } from './pages/SearchPage'
import { UploadPage } from './pages/UploadPage'

/**
 * Root component: provides auth state, then wires client-side routing. `/login`
 * is public; everything else is gated by {@link RequireAuth} and rendered under
 * the shared layout shell.
 */
export function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route element={<RequireAuth />}>
            <Route element={<Layout />}>
              <Route path="/" element={<HomePage />} />
              <Route path="/library" element={<LibraryPage />} />
              <Route path="/search" element={<SearchPage />} />
              {/* Uploading is a write action: editors and admins only. */}
              <Route element={<RequireRole role="editor" />}>
                <Route path="/upload" element={<UploadPage />} />
              </Route>
              <Route path="/account" element={<AccountPage />} />
              <Route path="*" element={<NotFoundPage />} />
            </Route>
          </Route>
        </Routes>
      </AuthProvider>
    </BrowserRouter>
  )
}
