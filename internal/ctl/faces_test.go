package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// facesBody is a realistic GET /photos/{uid}/faces body: one named detection, one
// unnamed one carrying suggestions, and one marker a person drew where the
// detector saw nothing — which is what a negative face_index means.
const facesBody = `{"photo_uid":"pht01","width":4000,"height":3000,"orientation":1,"faces":[
	{"face_index":0,"bbox":[0.1,0.2,0.3,0.4],"det_score":0.94,"action":"already_done",
	 "marker_uid":"mrk01","subject_uid":"sub01","subject_name":"Anna","iou":0.81,"suggestions":[]},
	{"face_index":1,"bbox":[0.5,0.2,0.2,0.25],"det_score":0.88,"action":"create_marker",
	 "suggestions":[{"subject_uid":"sub02","subject_name":"Bob","distance":0.31,"confidence":0.69}]},
	{"face_index":-1,"bbox":[0.7,0.7,0.1,0.1],"action":"assign_person","marker_uid":"mrk09",
	 "suggestions":[]}
]}`

// decodedFaces decodes facesBody, failing the test if it does not parse.
func decodedFaces(t *testing.T) FaceList {
	t.Helper()

	list, err := DecodeFaceList(json.RawMessage(facesBody))
	if err != nil {
		t.Fatalf("DecodeFaceList returned %v", err)
	}
	return list
}

// TestClient_ListFaces verifies the face listing reaches the wire path the API
// serves and decodes with the frame, the assignments and the suggestions intact.
func TestClient_ListFaces(t *testing.T) {
	t.Parallel()

	var gotPath, gotMethod string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Write([]byte(facesBody))
	})

	raw, err := client.ListFaces(t.Context(), "pht01")
	if err != nil {
		t.Fatalf("ListFaces returned %v", err)
	}
	if gotPath != "/api/v1/photos/pht01/faces" || gotMethod != http.MethodGet {
		t.Errorf("request = %s %s, want GET /api/v1/photos/pht01/faces", gotMethod, gotPath)
	}
	list, err := DecodeFaceList(raw)
	if err != nil {
		t.Fatalf("DecodeFaceList returned %v", err)
	}
	if list.PhotoUID != "pht01" || list.Width != 4000 || len(list.Faces) != 3 {
		t.Fatalf("face list = %+v, want the three faces of pht01 with its frame", list)
	}
	if !list.Faces[0].Named() || list.Faces[0].SubjectName != "Anna" {
		t.Errorf("first face = %+v, want Anna named on marker mrk01", list.Faces[0])
	}
	if list.Faces[1].Named() || len(list.Faces[1].Suggestions) != 1 {
		t.Errorf("second face = %+v, want an unnamed face with one suggestion", list.Faces[1])
	}
}

// TestClient_ListFaces_blankUID verifies a blank photo uid is refused before a
// request is spent on it.
func TestClient_ListFaces_blankUID(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("a blank uid reached the server")
	})
	if _, err := client.ListFaces(t.Context(), "  "); !errors.Is(err, ErrEmptyUID) {
		t.Errorf("ListFaces(blank) = %v, want ErrEmptyUID", err)
	}
}

// TestFaceList_Find verifies a face is found by its per-photo index, negative ones
// included, and that a missing index reports which indexes the photo does have.
func TestFaceList_Find(t *testing.T) {
	t.Parallel()

	list := decodedFaces(t)
	face, err := list.Find(-1)
	if err != nil || face.MarkerUID != "mrk09" {
		t.Errorf("Find(-1) = %+v, %v, want the hand-drawn marker", face, err)
	}
	_, err = list.Find(7)
	if !errors.Is(err, ErrUnknownFace) {
		t.Fatalf("Find(7) = %v, want ErrUnknownFace", err)
	}
	if !strings.Contains(err.Error(), "0, 1, -1") {
		t.Errorf("error %q does not list the indexes the photo has", err)
	}
}

// TestFaceList_Find_noFaces verifies a photo the detector has never looked at says
// so, rather than offering an empty list of indexes.
func TestFaceList_Find_noFaces(t *testing.T) {
	t.Parallel()

	_, err := FaceList{PhotoUID: "pht09"}.Find(0)
	if !errors.Is(err, ErrUnknownFace) || !strings.Contains(err.Error(), "nothing has been detected") {
		t.Errorf("Find on an empty photo = %v, want an explanatory ErrUnknownFace", err)
	}
}

// TestFaceView_AssignTo verifies the two routings: a detection that already
// carries a marker names that marker, one that does not gets a marker drawn over
// its own box.
func TestFaceView_AssignTo(t *testing.T) {
	t.Parallel()

	list := decodedFaces(t)
	named := list.Faces[0].AssignTo(SubjectRef{UID: "sub02"})
	if named.Action != FaceActionAssignPerson || named.MarkerUID != "mrk01" || named.BBox != nil {
		t.Errorf("assign onto an existing marker = %+v, want assign_person on mrk01", named)
	}
	if named.SubjectUID != "sub02" || named.SubjectName != "" {
		t.Errorf("assign by uid = %+v, want only the uid sent", named)
	}

	fresh := list.Faces[1].AssignTo(SubjectRef{Name: "Bob"})
	if fresh.Action != FaceActionCreateMarker || fresh.MarkerUID != "" {
		t.Errorf("assign onto a bare detection = %+v, want create_marker", fresh)
	}
	if fresh.FaceIndex == nil || *fresh.FaceIndex != 1 || fresh.BBox == nil ||
		*fresh.BBox != [4]float64{0.5, 0.2, 0.2, 0.25} {
		t.Errorf("create_marker = %+v, want the detection's own index and box", fresh)
	}
	if fresh.SubjectName != "Bob" || fresh.SubjectUID != "" {
		t.Errorf("assign by name = %+v, want only the name sent", fresh)
	}
}

// TestFaceView_Detach verifies a named face detaches by marker, and the two ways a
// detach cannot apply are refused locally with distinguishable errors.
func TestFaceView_Detach(t *testing.T) {
	t.Parallel()

	list := decodedFaces(t)
	detach, err := list.Faces[0].Detach()
	if err != nil {
		t.Fatalf("Detach on a named face returned %v", err)
	}
	if detach.Action != FaceActionUnassignPerson || detach.MarkerUID != "mrk01" {
		t.Errorf("detach = %+v, want unassign_person on mrk01", detach)
	}
	if _, err := list.Faces[1].Detach(); !errors.Is(err, ErrFaceNotMarked) {
		t.Errorf("detach of an unmarked detection = %v, want ErrFaceNotMarked", err)
	}
	if _, err := list.Faces[2].Detach(); !errors.Is(err, ErrFaceNotNamed) {
		t.Errorf("detach of an unnamed marker = %v, want ErrFaceNotNamed", err)
	}
}

// TestClient_AssignFace verifies the assignment body reaches the assign endpoint
// unchanged and its result decodes, subject included.
func TestClient_AssignFace(t *testing.T) {
	t.Parallel()

	var gotPath, gotMethod string
	var gotBody map[string]any
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		w.Write([]byte(`{"action":"assign_person",
			"marker":{"uid":"mrk01","photo_uid":"pht01","subject_uid":"sub02","type":"face",
			          "x":0.1,"y":0.2,"w":0.3,"h":0.4,"reviewed":true},
			"subject":{"uid":"sub02","name":"Bob","type":"person"}}`))
	})

	raw, err := client.AssignFace(t.Context(), "pht01",
		FaceAssignment{Action: FaceActionAssignPerson, MarkerUID: "mrk01", SubjectUID: "sub02"})
	if err != nil {
		t.Fatalf("AssignFace returned %v", err)
	}
	if gotPath != "/api/v1/photos/pht01/faces/assign" || gotMethod != http.MethodPost {
		t.Errorf("request = %s %s, want the assign endpoint", gotMethod, gotPath)
	}
	if gotBody["action"] != FaceActionAssignPerson || gotBody["marker_uid"] != "mrk01" {
		t.Errorf("body = %v, want the assignment unchanged", gotBody)
	}
	if _, present := gotBody["bbox"]; present {
		t.Errorf("body = %v, want no bbox on an assign_person", gotBody)
	}
	result, err := DecodeFaceAssign(raw)
	if err != nil {
		t.Fatalf("DecodeFaceAssign returned %v", err)
	}
	if result.Subject == nil || result.Subject.Name != "Bob" || result.Marker.UID != "mrk01" {
		t.Errorf("assign result = %+v, want the marker and Bob", result)
	}
}

// TestDecodeFaceAssign_detach verifies a detach's nil subject survives decoding,
// which is what makes "names nobody" distinguishable from "names someone unnamed".
func TestDecodeFaceAssign_detach(t *testing.T) {
	t.Parallel()

	result, err := DecodeFaceAssign(json.RawMessage(
		`{"action":"unassign_person","marker":{"uid":"mrk01","photo_uid":"pht01","type":"face"}}`))
	if err != nil {
		t.Fatalf("DecodeFaceAssign returned %v", err)
	}
	if result.Subject != nil {
		t.Errorf("subject = %+v, want nil after a detach", result.Subject)
	}
}

// faceOpinionCall is one of the four feedback methods, for the table below.
type faceOpinionCall = func(context.Context, FaceOpinion) error

// TestClient_faceFeedback verifies the four opinion endpoints each reach their own
// path with the right verb, and that all four carry the body — the DELETE half
// included, which is unusual enough to be worth pinning.
func TestClient_faceFeedback(t *testing.T) {
	t.Parallel()

	type call struct {
		name   string
		invoke func(*Client) func(context.Context, FaceOpinion) error
		path   string
		method string
	}
	calls := []call{
		{"reject", func(c *Client) faceOpinionCall { return c.RejectFace },
			"/api/v1/feedback/face-rejections", http.MethodPost},
		{"unreject", func(c *Client) faceOpinionCall { return c.UnrejectFace },
			"/api/v1/feedback/face-rejections", http.MethodDelete},
		{"confirm", func(c *Client) faceOpinionCall { return c.ConfirmFace },
			"/api/v1/feedback/face-confirmations", http.MethodPost},
		{"unconfirm", func(c *Client) faceOpinionCall { return c.UnconfirmFace },
			"/api/v1/feedback/face-confirmations", http.MethodDelete},
	}
	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			var gotPath, gotMethod string
			var gotBody map[string]any
			client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
				gotPath, gotMethod = r.URL.Path, r.Method
				body, _ := io.ReadAll(r.Body)
				json.Unmarshal(body, &gotBody)
				w.WriteHeader(http.StatusNoContent)
			})

			opinion := FaceOpinion{PhotoUID: "pht01", FaceIndex: 2, SubjectUID: "sub01"}
			if err := c.invoke(client)(t.Context(), opinion); err != nil {
				t.Fatalf("%s returned %v", c.name, err)
			}
			if gotPath != c.path || gotMethod != c.method {
				t.Errorf("request = %s %s, want %s %s", gotMethod, gotPath, c.method, c.path)
			}
			if gotBody["photo_uid"] != "pht01" || gotBody["subject_uid"] != "sub01" {
				t.Errorf("body = %v, want the whole opinion, DELETE included", gotBody)
			}
		})
	}
}

// TestFaceOpinion_validate verifies the three inputs the API would only answer with
// a 400 are refused before a request is spent.
func TestFaceOpinion_validate(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		opinion FaceOpinion
		want    error
	}{
		"blank photo":   {FaceOpinion{SubjectUID: "sub01"}, ErrEmptyUID},
		"blank subject": {FaceOpinion{PhotoUID: "pht01"}, ErrEmptyUID},
		"negative face": {FaceOpinion{PhotoUID: "pht01", SubjectUID: "sub01", FaceIndex: -1}, ErrNegativeFaceIndex},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client := testClient(t, "kkt_a_b", func(_ http.ResponseWriter, _ *http.Request) {
				t.Error("an invalid opinion reached the server")
			})
			if err := client.RejectFace(t.Context(), tc.opinion); !errors.Is(err, tc.want) {
				t.Errorf("RejectFace = %v, want %v", err, tc.want)
			}
		})
	}
}
