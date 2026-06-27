import { BrowserRouter, Route, Routes } from 'react-router-dom'

import { AuthProvider } from './auth/AuthProvider'
import { RequireAuth, RequireRole } from './auth/ProtectedRoute'
import { Layout } from './components/Layout'
import { AccountPage } from './pages/AccountPage'
import { AlbumDetailPage } from './pages/AlbumDetailPage'
import { AlbumsPage } from './pages/AlbumsPage'
import { ClustersPage } from './pages/ClustersPage'
import { HomePage } from './pages/HomePage'
import { LabelDetailPage } from './pages/LabelDetailPage'
import { LabelsPage } from './pages/LabelsPage'
import { LibraryPage } from './pages/LibraryPage'
import { LoginPage } from './pages/LoginPage'
import { MapPage } from './pages/MapPage'
import { NotFoundPage } from './pages/NotFoundPage'
import { PeoplePage } from './pages/PeoplePage'
import { PhotoDetailPage } from './pages/PhotoDetailPage'
import { SearchPage } from './pages/SearchPage'
import { SubjectPage } from './pages/SubjectPage'
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
              <Route path="/albums" element={<AlbumsPage />} />
              <Route path="/albums/:uid" element={<AlbumDetailPage />} />
              <Route path="/labels" element={<LabelsPage />} />
              <Route path="/labels/:uid" element={<LabelDetailPage />} />
              <Route path="/search" element={<SearchPage />} />
              <Route path="/map" element={<MapPage />} />
              <Route path="/photos/:uid" element={<PhotoDetailPage />} />
              <Route path="/people" element={<PeoplePage />} />
              <Route path="/people/:uid" element={<SubjectPage />} />
              {/* Uploading and cluster review are write actions: editors and admins only. */}
              <Route element={<RequireRole role="editor" />}>
                <Route path="/upload" element={<UploadPage />} />
                <Route path="/people/clusters" element={<ClustersPage />} />
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
