import { BrowserRouter, Route, Routes } from 'react-router-dom'

import { AuthProvider } from './auth/AuthProvider'
import { RequireAuth, RequireImport, RequireRole } from './auth/ProtectedRoute'
import { CapabilitiesProvider } from './capabilities/CapabilitiesProvider'
import { Layout } from './components/Layout'
import { PwaStatus } from './components/pwa/PwaStatus'
import { ToastProvider } from './components/toast/ToastProvider'
import { ACTIVITY_PATH } from './lib/activityView'
import { AccountPage } from './pages/AccountPage'
import { AlbumDetailPage } from './pages/AlbumDetailPage'
import { AlbumsPage } from './pages/AlbumsPage'
import { AuditPage } from './pages/AuditPage'
import { ClustersPage } from './pages/ClustersPage'
import { DupComparePage } from './pages/DupComparePage'
import { DuplicateMarkersPage } from './pages/DuplicateMarkersPage'
import { DuplicatesPage } from './pages/DuplicatesPage'
import { ExpandPage } from './pages/ExpandPage'
import { FacesPage } from './pages/FacesPage'
import { FavoritesPage } from './pages/FavoritesPage'
import { HelpPage } from './pages/HelpPage'
import { ImportPage } from './pages/ImportPage'
import { LabelDetailPage } from './pages/LabelDetailPage'
import { LabelsPage } from './pages/LabelsPage'
import { LeaderboardPage } from './pages/LeaderboardPage'
import { LibraryPage } from './pages/LibraryPage'
import { LibraryRedirect } from './pages/LibraryRedirect'
import { LoginPage } from './pages/LoginPage'
import { MaintenancePage } from './pages/MaintenancePage'
import { MapPage } from './pages/MapPage'
import { MyActivityPage } from './pages/MyActivityPage'
import { NotFoundPage } from './pages/NotFoundPage'
import { OutliersPage } from './pages/OutliersPage'
import { PasswordResetPage } from './pages/PasswordResetPage'
import { PeoplePage } from './pages/PeoplePage'
import { PhotoDetailPage } from './pages/PhotoDetailPage'
import { PlacesPage } from './pages/PlacesPage'
import { RecognitionPage } from './pages/RecognitionPage'
import { RegisterPage } from './pages/RegisterPage'
import { ReviewDecisionsPage } from './pages/ReviewDecisionsPage'
import { ReviewPage } from './pages/ReviewPage'
import { SavedSearchesPage } from './pages/SavedSearchesPage'
import { SearchPage } from './pages/SearchPage'
import { ShareTargetPage } from './pages/ShareTargetPage'
import { SlideshowPage } from './pages/SlideshowPage'
import { StatsPage } from './pages/StatsPage'
import { SubjectPage } from './pages/SubjectPage'
import { SystemStatusPage } from './pages/SystemStatusPage'
import { TrashPage } from './pages/TrashPage'
import { UploadPage } from './pages/UploadPage'
import { UsersPage } from './pages/UsersPage'

/**
 * The app's route table. `/login`, `/register` and `/password-reset/:token` are
 * public; everything else is gated by {@link RequireAuth} and rendered under the
 * shared layout shell. Exported apart from {@link App} so tests can mount it
 * inside a `MemoryRouter` and assert on the wiring itself (which path renders
 * what, and where `/library` forwards to).
 */
export function AppRoutes() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      {/* Self-service registration, public like sign-in: the whole point is that
          nobody signed in yet. The page itself asks the instance whether
          registration is open and says so when it is not. */}
      <Route path="/register" element={<RegisterPage />} />
      {/* The landing page of a password-reset link, public for the same reason:
          whoever follows it is locked out of the account it belongs to. The page
          asks the backend whether the link is still usable before it shows a
          form, and never signs anybody in. */}
      <Route path="/password-reset/:token" element={<PasswordResetPage />} />
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
          {/* Retired route, kept so old links and bookmarks resolve. The splat
              also catches the addresses inherited from the PhotoPrism instance
              this one replaced, which lived under `/library/…`. */}
          <Route path="/library/*" element={<LibraryRedirect />} />
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
          {/* Where the phone's share sheet lands. Deliberately outside the
              editor gate: it has to greet a viewer with an explanation of its
              own (their shared photos are discarded, not silently dropped),
              and it forwards an editor to /upload with the staged share. */}
          <Route path="/share-target" element={<ShareTargetPage />} />
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
            {/* Fixing a person tagged twice on one photo detaches markers: a
                write action, editors and admins only. */}
            <Route path="/duplicate-markers" element={<DuplicateMarkersPage />} />
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
            {/* One user's review decisions, reached by clicking a player on the
                leaderboard: inspecting who sorted what is governance, admin only. */}
            <Route path="/audit/reviews" element={<ReviewDecisionsPage />} />
          </Route>
          <Route path="/account" element={<AccountPage />} />
          {/* One user's own actions, from the audit trail narrowed to them by the
              server. No role gate: it is self-repair ("what did I just click
              wrong?"), not the governance view of everybody, which stays admin
              only at /audit. */}
          <Route path={ACTIVITY_PATH} element={<MyActivityPage />} />
          {/* Library statistics: read-only aggregate counts, so any signed-in
              role may open them — no role gate, like the leaderboard. */}
          <Route path="/stats" element={<StatsPage />} />
          {/* End-user help: no role guard — visible to any authenticated user. */}
          <Route path="/help" element={<HelpPage />} />
          <Route path="*" element={<NotFoundPage />} />
        </Route>
      </Route>
    </Routes>
  )
}

/** Root component: provides auth and capability state, then wires client-side routing. */
export function App() {
  return (
    <BrowserRouter>
      <ToastProvider>
        <AuthProvider>
          {/* Capabilities are only meaningful once authenticated (the endpoint is
              behind auth), so the provider sits inside AuthProvider. */}
          <CapabilitiesProvider>
            <AppRoutes />
          </CapabilitiesProvider>
        </AuthProvider>
        {/* Outside the auth gate and outside the layout shell: the service
            worker registers on every page load, and "you are offline" has to
            reach the login screen and the immersive routes as well. */}
        <PwaStatus />
      </ToastProvider>
    </BrowserRouter>
  )
}
