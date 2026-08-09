package review

// The three question kinds that check work the machine already did: the place
// check over estimated locations, the duplicate check over near-duplicate pairs,
// and the outlier check over faces that sit far from the person they are
// assigned to.
//
// They are collected the same way the face and label searches are — a bounded,
// rotating window per rebuild, with the cursor advancing so successive rebuilds
// walk the whole library — and for the same reason: the game shows one question
// at a time, so filling a batch must never cost a library-wide work list. Each
// one degrades to "no questions of this kind" rather than failing the rebuild,
// because a library with no estimated locations is a perfectly normal library
// and the other four kinds still have work.
//
// None of them goes through the confidence tiers. The tiers exist to mix
// one-click confirmations with genuinely uncertain guesses, and a tier is only
// meaningful where there is a single comparable confidence axis; "the estimator
// guessed Brno" and "these two files are 0.02 apart" are not points on the same
// scale. Each kind therefore carries its own ordering — most suspicious first —
// and the interleave, not the blend, is what keeps a batch mixed.

import (
	"context"
	"fmt"
	"math"

	"github.com/panbotka/kukatko/internal/candidates"
	"github.com/panbotka/kukatko/internal/duplicates"
	"github.com/panbotka/kukatko/internal/outliers"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
)

// placeQuestions reads one rotating window of the photos carrying an estimated
// location and turns them into place questions. It also returns the full size of
// that set (not the window's), for the empty-queue reason.
//
// A photo whose coordinates have not been reverse geocoded is skipped: the
// question is "was this taken in <place>?", and a pair of decimal degrees is not
// a place anybody can answer about. Those photos come back once the `places` job
// has caught up — which is exactly the right time to ask.
func (s *Service) placeQuestions(ctx context.Context, need int) ([]Question, int, error) {
	if s.places == nil {
		return nil, 0, nil
	}
	offset := s.placeOffset()
	pending, total, err := s.places.Pending(ctx, offset, need)
	if err != nil {
		return nil, 0, fmt.Errorf("review: listing estimated locations: %w", err)
	}
	s.advancePlaceOffset(wrapOffset(offset+len(pending), total))

	questions := make([]Question, 0, len(pending))
	for _, estimate := range pending {
		name := estimate.PlaceLabel()
		photo := estimate.Photo
		if name == "" || photo.Lat == nil || photo.Lng == nil {
			continue
		}
		s.media.DecorateOne(&photo)
		questions = append(questions, Question{
			ID:    placeQuestionID(photo.UID),
			Kind:  KindPlace,
			Photo: photo,
			Place: &PlaceGuess{
				Name:      name,
				Country:   estimate.Place.Country,
				City:      estimate.Place.City,
				PlaceName: estimate.Place.PlaceName,
				Lat:       *photo.Lat,
				Lng:       *photo.Lng,
			},
		})
	}
	return questions, total, nil
}

// duplicateQuestions reads one rotating page of near-duplicate groups and turns
// the two-member ones into side-by-side questions. It also returns how many
// groups the library holds in total, for the empty-queue reason.
//
// Only pairs — groups of exactly two — become questions, and that restriction is
// load-bearing rather than a simplification. A "no" is recorded as a dismissal of
// the edge, and a two-member group with its one edge dismissed disappears from
// every later scan, so the pair can never be asked about twice. Inside a larger
// component a dismissed edge does not remove the group, and the detector does not
// report which of its edges a user has already settled — so the game would
// happily re-ask a question somebody has answered, forever. Larger groups are
// also not a yes/no in the first place: they are a "which of these five do I
// keep?", which is what the duplicates page is for.
//
// A group already carrying a human confirmation is skipped for the same reason:
// the question has been answered.
func (s *Service) duplicateQuestions(ctx context.Context, need int) ([]Question, int, error) {
	if s.duplicates == nil || s.photos == nil {
		return nil, 0, nil
	}
	offset := s.dupOffset()
	res, err := s.duplicates.FindGroups(ctx, need, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("review: finding duplicate groups: %w", err)
	}
	s.advanceDupOffset(wrapOffset(offset+len(res.Groups), res.Total))

	pairs := pairGroups(res.Groups)
	byUID, err := s.photosByUID(ctx, pairUIDs(pairs))
	if err != nil {
		return nil, 0, err
	}
	questions := make([]Question, 0, len(pairs))
	for _, group := range pairs {
		question, ok := s.duplicateQuestion(group, byUID)
		if ok {
			questions = append(questions, question)
		}
	}
	sortByConfidence(questions)
	return questions, res.Total, nil
}

// pairGroups keeps the duplicate groups the game may ask about: exactly two
// members and no human verdict yet.
func pairGroups(groups []duplicates.Group) []duplicates.Group {
	kept := make([]duplicates.Group, 0, len(groups))
	for _, group := range groups {
		if len(group.Members) == 2 && !group.Confirmed {
			kept = append(kept, group)
		}
	}
	return kept
}

// pairUIDs collects every member uid of the pair groups, for one batch photo
// fetch.
func pairUIDs(groups []duplicates.Group) []string {
	uids := make([]string, 0, 2*len(groups))
	for _, group := range groups {
		for _, member := range group.Members {
			uids = append(uids, member.UID)
		}
	}
	return uids
}

// duplicateQuestion builds one side-by-side question from a two-member group.
// The keeper is shown first, because that is the photo the duplicates page would
// keep and therefore the one the player is comparing against. It reports ok=false
// when either photo has gone since the scan.
func (s *Service) duplicateQuestion(
	group duplicates.Group, byUID map[string]photos.Photo,
) (Question, bool) {
	keeper, other := group.Members[0], group.Members[1]
	if other.IsKeeper {
		keeper, other = other, keeper
	}
	first, ok := byUID[keeper.UID]
	if !ok {
		return Question{}, false
	}
	second, ok := byUID[other.UID]
	if !ok {
		return Question{}, false
	}
	s.media.DecorateOne(&first)
	s.media.DecorateOne(&second)
	return Question{
		ID:         duplicateQuestionID(first.UID, second.UID),
		Kind:       KindDuplicate,
		Confidence: pairConfidence(other),
		Photo:      first,
		Other:      &second,
		GroupID:    group.ID,
	}, true
}

// pairConfidence turns a non-keeper member's distances to the keeper into the
// 0–1 confidence the game shows. Both signals are optional and they are not
// commensurable, so the stronger of the two wins: a pair the embeddings call
// identical is a strong match even if its pHashes differ, and vice versa. With
// neither signal present the pair is reported at zero rather than invented.
func pairConfidence(member duplicates.Member) float64 {
	confidence := 0.0
	if member.PhashDistance != nil {
		// A 64-bit perceptual hash: every differing bit is 1/64 of the distance.
		confidence = math.Max(confidence, 1-float64(*member.PhashDistance)/64)
	}
	if member.EmbeddingDistance != nil {
		confidence = math.Max(confidence, 1-*member.EmbeddingDistance)
	}
	return math.Max(0, math.Min(1, confidence))
}

// outlierQuestions ranks a bounded, rotating window of the named subjects and
// turns their most distant faces into "is this really X?" questions. It also
// returns how many subjects the library holds, for the empty-queue reason.
//
// Only a face with a marker is asked about: a "no" detaches the person through
// the assign state machine, and a face with no marker is not tied to the subject
// through one, so there would be nothing to detach. Only a meaningful ranking is
// used either: with fewer than outliers.MinMeaningful faces every face is
// roughly equidistant from the centroid, so "furthest" means nothing and the
// question would be an accusation drawn from noise.
func (s *Service) outlierQuestions(ctx context.Context, need int) ([]Question, int, error) {
	if s.outliers == nil || s.subjects == nil || s.photos == nil {
		return nil, 0, nil
	}
	subjects, err := s.outlierPlan(ctx)
	if err != nil {
		return nil, 0, err
	}
	if len(subjects) == 0 {
		return nil, 0, nil
	}
	offset := wrapOffset(s.outlierOffset(), len(subjects))
	found, scanned, err := s.rankWindow(ctx, rotateSubjects(subjects, offset, s.outlierBudget), need)
	if err != nil {
		return nil, 0, err
	}
	// The cursor advances by what was actually ranked, not by the whole window: a
	// scan that stopped early because the batch was full has not looked at the
	// rest of its window, and skipping them would leave those people unasked
	// about until the cursor came all the way round again.
	s.advanceOutlierOffset(wrapOffset(offset+scanned, len(subjects)))
	questions, err := s.outlierQuestionsFor(ctx, found)
	if err != nil {
		return nil, 0, err
	}
	return questions, len(subjects), nil
}

// rankWindow ranks the subjects of one window until it holds need faces worth
// asking about, and reports how many of them it actually looked at.
func (s *Service) rankWindow(
	ctx context.Context, window []people.Subject, need int,
) ([]subjectOutliers, int, error) {
	var found []subjectOutliers
	scanned, collected := 0, 0
	for _, subject := range window {
		if collected >= need {
			break
		}
		theirs, err := s.rankSubject(ctx, subject)
		if err != nil {
			return nil, 0, err
		}
		scanned++
		if len(theirs.faces) == 0 {
			continue
		}
		found = append(found, theirs)
		collected += len(theirs.faces)
	}
	return found, scanned, nil
}

// subjectOutliers pairs one subject with the faces of theirs worth asking about.
type subjectOutliers struct {
	subject people.Subject
	faces   []outliers.OutlierFace
}

// outlierPlan lists the subjects the outlier rotation may walk: those with at
// least MinMeaningful markers, since a ranking over fewer faces carries no
// signal and would only produce questions drawn from noise.
func (s *Service) outlierPlan(ctx context.Context) ([]people.Subject, error) {
	all, err := s.subjects.ListSubjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("review: listing subjects for outliers: %w", err)
	}
	plan := make([]people.Subject, 0, len(all))
	for _, subject := range all {
		if subject.MarkerCount >= outliers.MinMeaningful {
			plan = append(plan, subject.Subject)
		}
	}
	return plan, nil
}

// rankSubject ranks one subject's faces and keeps the ones the game may ask
// about: far enough from the centroid, and tied to a marker a "no" could detach.
// A single subject's ranking failing is logged and skipped rather than failing
// the rebuild, matching the per-label policy of the label scan.
func (s *Service) rankSubject(ctx context.Context, subject people.Subject) (subjectOutliers, error) {
	opts := outliers.Options{Threshold: s.outlierThreshold, Limit: s.maxPerEntity}
	res, err := s.outliers.Outliers(ctx, subject.UID, opts)
	if err != nil {
		if ctx.Err() != nil {
			return subjectOutliers{}, fmt.Errorf("review: ranking outliers of %s: %w", subject.UID, err)
		}
		s.log.WarnContext(ctx, "review: outlier ranking failed",
			"subject_uid", subject.UID, "error", err)
		return subjectOutliers{}, nil
	}
	if !res.Meaningful {
		return subjectOutliers{}, nil
	}
	kept := make([]outliers.OutlierFace, 0, len(res.Faces))
	for _, face := range res.Faces {
		if face.MarkerUID != "" {
			kept = append(kept, face)
		}
	}
	return subjectOutliers{subject: subject, faces: kept}, nil
}

// outlierQuestionsFor hydrates the photos of every kept face in one read and
// builds the questions, most suspicious (furthest from the centroid) first.
func (s *Service) outlierQuestionsFor(
	ctx context.Context, found []subjectOutliers,
) ([]Question, error) {
	var uids []string
	for _, entry := range found {
		for _, face := range entry.faces {
			uids = append(uids, face.PhotoUID)
		}
	}
	byUID, err := s.photosByUID(ctx, uids)
	if err != nil {
		return nil, err
	}
	var questions []Question
	for _, entry := range found {
		subject := entry.subject
		for _, face := range entry.faces {
			photo, ok := byUID[face.PhotoUID]
			if !ok || photo.HiddenFromLibrary {
				continue
			}
			s.media.DecorateOne(&photo)
			faceIndex := face.FaceIndex
			box := faceBoxOf(face.BBox, photo)
			questions = append(questions, Question{
				ID:         outlierQuestionID(photo.UID, face.FaceIndex, subject.UID),
				Kind:       KindOutlier,
				Confidence: math.Max(0, 1-face.Distance),
				Distance:   face.Distance,
				Photo:      photo,
				Subject:    &subject,
				FaceIndex:  &faceIndex,
				BBox:       &box,
				MarkerUID:  face.MarkerUID,
			})
		}
	}
	sortBySuspicion(questions)
	return questions, nil
}

// photosByUID loads a batch of photos keyed by uid, deduplicating the input so a
// photo named twice (both halves of a pair, two outlier faces on one shot) is
// fetched once. An empty input yields an empty map without a query.
func (s *Service) photosByUID(ctx context.Context, uids []string) (map[string]photos.Photo, error) {
	byUID := make(map[string]photos.Photo, len(uids))
	if len(uids) == 0 {
		return byUID, nil
	}
	seen := make(map[string]struct{}, len(uids))
	unique := make([]string, 0, len(uids))
	for _, uid := range uids {
		if _, ok := seen[uid]; ok {
			continue
		}
		seen[uid] = struct{}{}
		unique = append(unique, uid)
	}
	list, err := s.photos.ListByUIDs(ctx, unique)
	if err != nil {
		return nil, fmt.Errorf("review: loading photos: %w", err)
	}
	for _, photo := range list {
		byUID[photo.UID] = photo
	}
	return byUID, nil
}

// rotateSubjects returns the budget-long window of subjects starting at offset,
// wrapping past the end — the subject twin of rotateLabels. A non-positive
// budget, or one at least as large as the list, returns every subject, still
// starting at offset.
func rotateSubjects(subjects []people.Subject, offset, budget int) []people.Subject {
	total := len(subjects)
	if budget <= 0 || budget > total {
		budget = total
	}
	out := make([]people.Subject, 0, budget)
	for i := range budget {
		out = append(out, subjects[(offset+i)%total])
	}
	return out
}

// faceBoxOf projects a face's normalised box into the relative and display-pixel
// spaces the UI draws in, mirroring what the candidate search hands the face
// questions so both kinds carry the same shape.
func faceBoxOf(bbox [4]float64, photo photos.Photo) candidates.FaceBox {
	width, height := photo.FileWidth, photo.FileHeight
	// EXIF orientations 5–8 are the quarter turns: the stored dimensions are
	// transposed relative to what the browser paints, and the box is normalised
	// against the painted frame.
	if photo.FileOrientation >= 5 && photo.FileOrientation <= 8 {
		width, height = height, width
	}
	return candidates.FaceBox{
		Relative: bbox,
		Pixel: [4]int{
			int(math.Round(bbox[0] * float64(width))),
			int(math.Round(bbox[1] * float64(height))),
			int(math.Round(bbox[2] * float64(width))),
			int(math.Round(bbox[3] * float64(height))),
		},
	}
}
