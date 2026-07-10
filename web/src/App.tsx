import { BrowserRouter, Route, Routes } from 'react-router-dom'

import { AuthProvider } from './auth/AuthProvider'
import { RequireAuth, RequireRole } from './auth/ProtectedRoute'
import { Layout } from './components/Layout'
import { AccountPage } from './pages/AccountPage'
import { AlbumDetailPage } from './pages/AlbumDetailPage'
import { AlbumsPage } from './pages/AlbumsPage'
import { ClustersPage } from './pages/ClustersPage'
import { DuplicatesPage } from './pages/DuplicatesPage'
import { FavoritesPage } from './pages/FavoritesPage'
import { ImportPage } from './pages/ImportPage'
import { LabelDetailPage } from './pages/LabelDetailPage'
import { LabelsPage } from './pages/LabelsPage'
import { LibraryPage } from './pages/LibraryPage'
import { LibraryRedirect } from './pages/LibraryRedirect'
import { LoginPage } from './pages/LoginPage'
import { MaintenancePage } from './pages/MaintenancePage'
import { MapPage } from './pages/MapPage'
import { NotFoundPage } from './pages/NotFoundPage'
import { PeoplePage } from './pages/PeoplePage'
import { PhotoDetailPage } from './pages/PhotoDetailPage'
import { PlacesPage } from './pages/PlacesPage'
import { SavedSearchesPage } from './pages/SavedSearchesPage'
import { SearchPage } from './pages/SearchPage'
import { SlideshowPage } from './pages/SlideshowPage'
import { SubjectPage } from './pages/SubjectPage'
import { SystemStatusPage } from './pages/SystemStatusPage'
import { TrashPage } from './pages/TrashPage'
import { UploadPage } from './pages/UploadPage'

/**
 * The app's route table. `/login` is public; everything else is gated by
 * {@link RequireAuth} and rendered under the shared layout shell. Exported apart
 * from {@link App} so tests can mount it inside a `MemoryRouter` and assert on
 * the wiring itself (which path renders what, and where `/library` forwards to).
 */
export function AppRoutes() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route element={<RequireAuth />}>
        {/* Fullscreen slideshow lives outside the layout shell (no navbar). */}
        <Route path="/slideshow" element={<SlideshowPage />} />
        <Route element={<Layout />}>
          {/* The photo library is the homepage: the catalog is what the app is
              for, so it greets the user rather than hiding one click away. */}
          <Route path="/" element={<LibraryPage />} />
          {/* Retired route, kept so old links and bookmarks resolve. */}
          <Route path="/library" element={<LibraryRedirect />} />
          <Route path="/favorites" element={<FavoritesPage />} />
          <Route path="/albums" element={<AlbumsPage />} />
          <Route path="/albums/:uid" element={<AlbumDetailPage />} />
          <Route path="/labels" element={<LabelsPage />} />
          <Route path="/labels/:uid" element={<LabelDetailPage />} />
          <Route path="/search" element={<SearchPage />} />
          <Route path="/saved" element={<SavedSearchesPage />} />
          <Route path="/map" element={<MapPage />} />
          <Route path="/places" element={<PlacesPage />} />
          <Route path="/photos/:uid" element={<PhotoDetailPage />} />
          <Route path="/people" element={<PeoplePage />} />
          <Route path="/people/:uid" element={<SubjectPage />} />
          {/* Uploading and cluster review are write actions: editors and admins only. */}
          <Route element={<RequireRole role="editor" />}>
            <Route path="/upload" element={<UploadPage />} />
            <Route path="/people/clusters" element={<ClustersPage />} />
            {/* Duplicate review archives photos in bulk: editors and admins only. */}
            <Route path="/duplicates" element={<DuplicatesPage />} />
            {/* Trash management (restore / permanent delete) is a write action. */}
            <Route path="/trash" element={<TrashPage />} />
          </Route>
          {/* Import/migration administration is admin-only. */}
          <Route element={<RequireRole role="admin" />}>
            <Route path="/import" element={<ImportPage />} />
            <Route path="/maintenance" element={<MaintenancePage />} />
            <Route path="/system" element={<SystemStatusPage />} />
          </Route>
          <Route path="/account" element={<AccountPage />} />
          <Route path="*" element={<NotFoundPage />} />
        </Route>
      </Route>
    </Routes>
  )
}

/** Root component: provides auth state, then wires client-side routing. */
export function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <AppRoutes />
      </AuthProvider>
    </BrowserRouter>
  )
}
