package ctl

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// suggestWidth keeps a face's suggested identities on one terminal row. The whole
// ranked list, distances included, is in `-o json` and `-o llm`.
const suggestWidth = 34

// SubjectLabel renders a person the way every recognition command reports one:
// the name first, because a uid alone is unreadable, and the uid in brackets,
// because the name is what the next command cannot be given. An unnamed face is
// a dash — it is not "a person called nothing".
func SubjectLabel(name, uid string) string {
	return NamedUID(name, uid)
}

// formatBBox renders a normalised box as origin and size, at the three decimals a
// 0..1 coordinate is meaningful to on a photo of any size.
func formatBBox(bbox [4]float64) string {
	return fmt.Sprintf("%.3f,%.3f %.3f×%.3f", bbox[0], bbox[1], bbox[2], bbox[3])
}

// formatScore renders a detector confidence or a match confidence, dashing a zero
// — a marker somebody drew by hand has no detector score, and printing 0.00 would
// read as "the detector was certain it is not a face".
func formatScore(score float64) string {
	if score == 0 {
		return "-"
	}
	return strconv.FormatFloat(score, 'f', 2, 64)
}

// WriteFaceList renders a photo's detections as a compact table: who each one
// names, the marker that carries the name, and — for the ones nobody has named —
// the detector's confidence and the identities the server suggests.
//
// A negative FACE index is not a mistake: it marks a box a person drew where the
// detector found nothing, which has no detection to be indexed by.
func WriteFaceList(w io.Writer, list FaceList) error {
	if len(list.Faces) == 0 {
		return writeLine(w, "no faces found on photo "+list.PhotoUID)
	}
	rows := make([][]string, 0, len(list.Faces))
	for _, face := range list.Faces {
		rows = append(rows, []string{
			strconv.Itoa(face.FaceIndex),
			elide(SubjectLabel(face.SubjectName, face.SubjectUID), nameWidth),
			dash(face.MarkerUID),
			formatScore(face.DetScore),
			formatBBox(face.BBox),
			dash(face.Action),
			elide(dash(formatSuggestions(face.Suggestions)), suggestWidth),
		})
	}
	header := []string{"FACE", "WHO", "MARKER", "SCORE", "BOX", "ACTION", "SUGGESTS"}
	if err := writeTable(w, header, rows); err != nil {
		return err
	}
	return writeLine(w, "\n"+faceSummary(list))
}

// formatSuggestions renders the suggested identities of one face, nearest first,
// each with the cosine distance that ranked it.
func formatSuggestions(suggestions []FaceSuggestion) string {
	parts := make([]string, 0, len(suggestions))
	for _, suggestion := range suggestions {
		parts = append(parts, suggestion.SubjectName+" "+
			formatDistance(suggestion.Distance))
	}
	return strings.Join(parts, ", ")
}

// faceSummary builds the one-line footer: how many faces the photo carries and how
// many of them still name nobody, which is the number that says whether there is
// work left on this photo.
func faceSummary(list FaceList) string {
	unnamed := 0
	for _, face := range list.Faces {
		if !face.Named() {
			unnamed++
		}
	}
	named := len(list.Faces) - unnamed
	return strings.Join([]string{
		"photo " + list.PhotoUID,
		strconv.Itoa(len(list.Faces)) + " " + plural(len(list.Faces), "face", "faces"),
		strconv.Itoa(named) + " named",
		strconv.Itoa(unnamed) + " unnamed",
	}, " · ")
}

// WriteFaceAssign renders the outcome of one assignment as a single line naming
// both the marker that changed and the person it now carries — a detach says so
// outright rather than printing an empty name.
func WriteFaceAssign(w io.Writer, result FaceAssignResult) error {
	marker := "marker " + dash(result.Marker.UID) + " on photo " + dash(result.Marker.PhotoUID)
	if result.Subject == nil {
		return writeLine(w, result.Action+": "+marker+" now names nobody")
	}
	return writeLine(w, result.Action+": "+marker+" now names "+
		SubjectLabel(result.Subject.Name, result.Subject.UID))
}

// WriteClusters renders the auto-clustered groups of unassigned faces: how big
// each group is, who the server thinks it is, and one face to look at.
func WriteClusters(w io.Writer, clusters []Cluster) error {
	if len(clusters) == 0 {
		return writeLine(w, "no clusters found")
	}
	faces := 0
	rows := make([][]string, 0, len(clusters))
	for _, group := range clusters {
		faces += group.Size
		rows = append(rows, []string{
			group.UID,
			strconv.Itoa(group.Size),
			elide(formatClusterSuggestion(group.Suggestion), nameWidth),
			group.Representative.PhotoUID + " #" + strconv.Itoa(group.Representative.FaceIndex),
			formatStamp(group.CreatedAt),
		})
	}
	header := []string{"UID", "FACES", "SUGGESTION", "REPRESENTATIVE", "CREATED"}
	if err := writeTable(w, header, rows); err != nil {
		return err
	}
	return writeLine(w, "\n"+strconv.Itoa(len(clusters))+" "+
		plural(len(clusters), "cluster", "clusters")+" · "+
		strconv.Itoa(faces)+" unnamed "+plural(faces, "face", "faces"))
}

// WriteClusterPage writes one page of the cluster listing: the table of groups,
// then what surrounds the page — how many groups the server is still preparing
// in the background, and the offset that asks for the next page. Both lines are
// omitted when there is nothing to say.
func WriteClusterPage(w io.Writer, page ClusterPage) error {
	if err := WriteClusters(w, page.Clusters); err != nil {
		return err
	}
	if page.Pending > 0 {
		if err := writeLine(w, strconv.Itoa(page.Pending)+" more "+
			plural(page.Pending, "group is", "groups are")+
			" still being prepared in the background; ask again in a moment"); err != nil {
			return err
		}
	}
	if page.NextOffset != nil {
		return writeLine(w, "next page: --offset "+strconv.Itoa(*page.NextOffset))
	}
	return nil
}

// formatClusterSuggestion renders the nearest named subject to a cluster with the
// cosine distance behind the guess, or a dash when nobody was close enough.
func formatClusterSuggestion(suggestion *FaceSuggestion) string {
	if suggestion == nil {
		return "-"
	}
	return SubjectLabel(suggestion.SubjectName, suggestion.SubjectUID) +
		" " + formatDistance(suggestion.Distance)
}

// WriteClusterAssign renders the outcome of naming a whole cluster: who it became
// and how many markers that wrote.
func WriteClusterAssign(w io.Writer, result ClusterAssignResult) error {
	return writeLine(w, "cluster "+dash(result.ClusterUID)+" assigned to "+
		SubjectLabel(result.Subject.Name, result.Subject.UID)+": "+
		strconv.Itoa(len(result.Markers))+" "+
		plural(len(result.Markers), "marker", "markers")+" written")
}

// WriteClusterRemoval renders what removing a stray face left behind: the smaller
// cluster, or the news that it was the last face and the cluster is gone.
func WriteClusterRemoval(w io.Writer, clusterUID string, cluster *Cluster) error {
	if cluster == nil {
		return writeLine(w, "cluster "+clusterUID+" held nothing else and was removed")
	}
	return writeLine(w, "cluster "+clusterUID+" now holds "+strconv.Itoa(cluster.Size)+
		" "+plural(cluster.Size, "face", "faces"))
}

// WriteMergeReport renders what a merge moved, with both people named.
//
// This is the one result ctl does not pass through unchanged in the machine
// formats (the other is the synthesized Ack of a 204). The reason is the merge
// itself: it deletes the subject it merged away, so after it has run the source's
// name exists nowhere — not in the library, not in the response, which carries
// only uids. Echoing the server's bytes would throw away the only account of who
// was merged into whom, on the one operation that cannot be undone.
func WriteMergeReport(w io.Writer, out Output, report MergeReport) error {
	if out.Format == FormatTable {
		return writeKeyValues(w, mergeRows(report))
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("encoding the merge result: %w", err)
	}
	if out.Format == FormatLLM {
		return WriteLLM(w, encoded, out.Fields)
	}
	return WriteJSON(w, encoded)
}

// mergeRows lists the key/value pairs of a merge report in display order.
func mergeRows(report MergeReport) [][2]string {
	return [][2]string{
		{"KEEPER", SubjectLabel(report.KeeperName, report.KeeperUID)},
		{"MERGED AWAY", SubjectLabel(report.SourceName, report.SourceUID)},
		{"MARKERS MOVED", strconv.Itoa(report.MarkersMoved)},
		{"FACES MOVED", strconv.Itoa(report.FacesMoved)},
		{"CONFIRMATIONS MOVED", strconv.Itoa(report.ConfirmationsMoved)},
		{"REJECTIONS MOVED", strconv.Itoa(report.RejectionsMoved)},
		{"REJECTIONS DROPPED", strconv.Itoa(report.RejectionsDropped)},
		{"DISMISSALS MOVED", strconv.Itoa(report.DismissalsMoved)},
		{"SHARED PHOTOS", strconv.Itoa(report.SharedPhotos)},
	}
}
