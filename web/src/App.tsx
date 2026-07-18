import { BrowserRouter, Route, Routes } from 'react-router-dom'

import { AuthProvider } from './auth/AuthProvider'
import { RequireAuth, RequireImport, RequireRole } from './auth/ProtectedRoute'
import { Layout } from './components/Layout'
import { ToastProvider } from './components/toast/ToastProvider'
import { AccountPage } from './pages/AccountPage'
import { AlbumDetailPage } from './pages/AlbumDetailPage'
import { AlbumsPage } from './pages/AlbumsPage'
import { AuditPage } from './pages/AuditPage'
import { ClustersPage } from './pages/ClustersPage'
import { DupComparePage } from './pages/DupComparePage'
import { DuplicatesPage } from './pages/DuplicatesPage'
import { ExpandPage } from './pages/ExpandPage'
import { FacesPage } from './pages/FacesPage'
import { FavoritesPage } from './pages/FavoritesPage'
import { ImportPage } from './pages/ImportPage'
import { LabelDetailPage } from './pages/LabelDetailPage'
import { LabelsPage } from './pages/LabelsPage'
import { LeaderboardPage } from './pages/LeaderboardPage'
import { LibraryPage } from './pages/LibraryPage'
import { LibraryRedirect } from './pages/LibraryRedirect'
import { LoginPage } from './pages/LoginPage'
import { MaintenancePage } from './pages/MaintenancePage'
import { MapPage } from './pages/MapPage'
import { NotFoundPage } from './pages/NotFoundPage'
import { OutliersPage } from './pages/OutliersPage'
import { PeoplePage } from './pages/PeoplePage'
import { PhotoDetailPage } from './pages/PhotoDetailPage'
import { PlacesPage } from './pages/PlacesPage'
import { RecognitionPage } from './pages/RecognitionPage'
import { ReviewPage } from './pages/ReviewPage'
import { SavedSearchesPage } from './pages/SavedSearchesPage'
import { SearchPage } from './pages/SearchPage'
import { SlideshowPage } from './pages/SlideshowPage'
import { SubjectPage } from './pages/SubjectPage'
import { SystemStatusPage } from './pages/SystemStatusPage'
import { TrashPage } from './pages/TrashPage'
import { UploadPage } from './pages/UploadPage'
import { UsersPage } from './pages/UsersPage'

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
        {/* The photo viewer is immersive full-bleed — the image owns the whole
            viewport — so it lives outside the shell too (no navbar/footer). */}
        <Route path="/photos/:uid" element={<PhotoDetailPage />} />
        {/* The review game is fullscreen too — one question must own the whole
            screen — and it writes, so it is editors and admins only. */}
        <Route element={<RequireRole role="editor" />}>
          <Route path="/review" element={<ReviewPage />} />
          {/* Comparing two duplicates needs the whole viewport — the decision is
              made by looking at the pixels — and it merges/archives, so it is
              editors and admins only, like the list it is reached from. */}
          <Route path="/duplicates/compare" element={<DupComparePage />} />
        </Route>
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
          <Route path="/people" element={<PeoplePage />} />
          <Route path="/people/:uid" element={<SubjectPage />} />
          {/* The sorting competition standings: read-only aggregate counts, so
              any signed-in role may watch the game — no write gate. */}
          <Route path="/leaderboard" element={<LeaderboardPage />} />
          {/* Uploading and cluster review are write actions: editors and admins only. */}
          <Route element={<RequireRole role="editor" />}>
            <Route path="/upload" element={<UploadPage />} />
            <Route path="/people/clusters" element={<ClustersPage />} />
            {/* Finding a person among untagged photos assigns faces: a write action. */}
            <Route path="/faces" element={<FacesPage />} />
            {/* Growing an album/label with similar photos adds members: a write action. */}
            <Route path="/expand" element={<ExpandPage />} />
            {/* The recognition sweep confirms faces across everyone: a write action. */}
            <Route path="/recognition" element={<RecognitionPage />} />
            {/* Reviewing a person's outliers unassigns faces: a write action. */}
            <Route path="/outliers" element={<OutliersPage />} />
            {/* Duplicate review archives photos in bulk: editors and admins only. */}
            <Route path="/duplicates" element={<DuplicatesPage />} />
            {/* Trash management (restore / permanent delete) is a write action. */}
            <Route path="/trash" element={<TrashPage />} />
          </Route>
          {/* Import/migration is an operations capability: maintainer only. */}
          <Route element={<RequireImport />}>
            <Route path="/import" element={<ImportPage />} />
          </Route>
          {/* Operations — library upkeep and system status — are maintainer only,
              the top of the role ladder. */}
          <Route element={<RequireRole role="maintainer" />}>
            <Route path="/maintenance" element={<MaintenancePage />} />
            <Route path="/system" element={<SystemStatusPage />} />
          </Route>
          {/* Governance — user management and the audit log — is admin or higher. */}
          <Route element={<RequireRole role="admin" />}>
            <Route path="/users" element={<UsersPage />} />
            <Route path="/audit" element={<AuditPage />} />
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
      <ToastProvider>
        <AuthProvider>
          <AppRoutes />
        </AuthProvider>
      </ToastProvider>
    </BrowserRouter>
  )
}
