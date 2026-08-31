package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// errBadFaceIndex indicates a face argument that is not a whole number.
var errBadFaceIndex = errors.New("the face must be given by its index, a whole number")

// parseFaceIndex reads the per-photo index that identifies a detection. It is not
// a uid: `ctl photos faces` prints it in the FACE column, and it is what every
// face command takes.
func parseFaceIndex(raw string) (int, error) {
	index, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", errBadFaceIndex, raw)
	}
	return index, nil
}

// newCtlPhotosFacesCmd builds "ctl photos faces <uid>": the recognition state of
// one photo, which is where every other face command starts.
func newCtlPhotosFacesCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "faces <uid>",
		Short: "List a photo's detected faces, who they name, and who they might be",
		Long: "List a photo's detected faces.\n\n" +
			"Each row is one detection: the person it names (if anyone), the marker that\n" +
			"carries the name, the detector's confidence, the box, and the identities the\n" +
			"server suggests for it. The FACE column is the index every other face command\n" +
			"takes; a negative one is a box a person drew where the detector saw nothing.\n\n" +
			"Unlike `photos get --people`, this asks the server for suggestions, which costs\n" +
			"a nearest-neighbour search per face — read it when you are about to name\n" +
			"somebody, not to take stock of the library.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.ListFaces(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("listing the faces of photo %s: %w", args[0], err)
			}
			return renderFaceList(cmd.OutOrStdout(), out, raw)
		},
	}
}

// newCtlFacesCmd builds the "ctl faces" tree: naming the detections on a photo,
// and recording the opinions that keep a refused suggestion refused.
func newCtlFacesCmd(opts *ctlOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "faces",
		Short: "Name the faces on a photo, and record what a suggestion got wrong",
		Long: "Name the faces on a photo (`ctl photos faces <uid>` lists them), and record the\n" +
			"opinions behind the suggestions.\n\n" +
			"A rejection and a confirmation are opinions, not edits: neither detaches a\n" +
			"marker nor draws one. A rejection keeps a wrong suggestion from coming back on\n" +
			"every sweep; a confirmation keeps a correct assignment out of the outlier\n" +
			"review. They are opposites — do not reach for one meaning the other.",
	}
	cmd.AddCommand(newCtlFacesAssignCmd(opts), newCtlFacesDetachCmd(opts))
	for _, spec := range faceOpinionSpecs() {
		cmd.AddCommand(newCtlFaceOpinionCmd(opts, spec))
	}
	return cmd
}

// newCtlFacesAssignCmd builds "ctl faces assign <photo-uid> <face> [<subject-uid>]".
func newCtlFacesAssignCmd(opts *ctlOptions) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "assign <photo-uid> <face> [<subject-uid>]",
		Short: "Attach one detected face to a person (editor or admin)",
		Long: "Attach one detected face to a person.\n\n" +
			"Name the person by uid, or with --name to have the server find the person of\n" +
			"that name and create them if the library has never heard of them.\n\n" +
			"Whether this draws a new marker or names one that is already there follows from\n" +
			"the photo's own face listing, which is read first; the assignment state machine\n" +
			"itself stays on the server.",
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			faceIndex, err := parseFaceIndex(args[1])
			if err != nil {
				return err
			}
			subject, err := ctl.SubjectRefFromArgs(positional(args, 2), name)
			if err != nil {
				return fmt.Errorf("reading the subject: %w", err)
			}
			return applyFaceAssignment(cmd, opts, args[0], faceIndex,
				func(face ctl.FaceView) (ctl.FaceAssignment, error) {
					return face.AssignTo(subject), nil
				})
		},
	}
	cmd.Flags().StringVar(&name, "name", "",
		"name the person instead of naming their uid; an unknown name creates the subject")
	return cmd
}

// newCtlFacesDetachCmd builds "ctl faces detach <photo-uid> <face>", the reverse
// of assign: the marker stays, the name it carried does not.
func newCtlFacesDetachCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "detach <photo-uid> <face>",
		Short: "Detach whoever a face names, leaving the marker unnamed (editor or admin)",
		Long: "Detach whoever a face names.\n\n" +
			"The marker survives, unnamed — the detection is still a face, it is just no\n" +
			"longer that person. A face nobody has named is refused locally rather than\n" +
			"sent, so a mistyped index does not read as a server error.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			faceIndex, err := parseFaceIndex(args[1])
			if err != nil {
				return err
			}
			return applyFaceAssignment(cmd, opts, args[0], faceIndex, ctl.FaceView.Detach)
		},
	}
}

// positional returns the argument at index, or an empty string when the caller
// left that optional argument out.
func positional(args []string, index int) string {
	if index >= len(args) {
		return ""
	}
	return args[index]
}

// applyFaceAssignment resolves one detection on a photo and sends the assignment
// build derives from it.
//
// The photo's faces are read first because only the server knows whether the
// detection already carries a marker, and that is what decides the action. It also
// means a face index the photo does not have fails against the listing, which can
// say which indexes it does have.
func applyFaceAssignment(
	cmd *cobra.Command, opts *ctlOptions, photoUID string, faceIndex int,
	build func(ctl.FaceView) (ctl.FaceAssignment, error),
) error {
	client, out, err := opts.resolve()
	if err != nil {
		return err
	}
	face, err := resolveFace(cmd.Context(), client, photoUID, faceIndex)
	if err != nil {
		return err
	}
	assignment, err := build(face)
	if err != nil {
		return fmt.Errorf("updating face %d of photo %s: %w", faceIndex, photoUID, err)
	}
	raw, err := client.AssignFace(cmd.Context(), photoUID, assignment)
	if err != nil {
		return fmt.Errorf("assigning face %d of photo %s: %w", faceIndex, photoUID, err)
	}
	return renderFaceAssign(cmd.OutOrStdout(), out, raw)
}

// resolveFace reads a photo's face listing and picks out the one detection.
func resolveFace(
	ctx context.Context, client *ctl.Client, photoUID string, faceIndex int,
) (ctl.FaceView, error) {
	raw, err := client.ListFaces(ctx, photoUID)
	if err != nil {
		return ctl.FaceView{}, fmt.Errorf("reading the faces of photo %s: %w", photoUID, err)
	}
	list, err := ctl.DecodeFaceList(raw)
	if err != nil {
		return ctl.FaceView{}, fmt.Errorf("reading the faces of photo %s: %w", photoUID, err)
	}
	face, err := list.Find(faceIndex)
	if err != nil {
		return ctl.FaceView{}, fmt.Errorf("reading face %d of photo %s: %w", faceIndex, photoUID, err)
	}
	return face, nil
}

// faceOpinion is one of the four feedback calls, bound to a resolved client.
type faceOpinion func(ctx context.Context, in ctl.FaceOpinion) error

// faceOpinionSpec is what distinguishes the four feedback commands: their help
// text, the phrase their confirmation line uses, and the client call.
type faceOpinionSpec struct {
	use   string
	short string
	long  string
	// past completes "face 2 of photo pht01 …", naming the person after it.
	past string
	pick func(*ctl.Client) faceOpinion
}

// faceOpinionSpecs lists the four persisted opinions about a face↔person pair.
// They come in undoable pairs on purpose: an opinion recorded by mistake is itself
// a mistake worth being able to take back.
func faceOpinionSpecs() []faceOpinionSpec {
	return []faceOpinionSpec{
		{
			use:   "reject <photo-uid> <face> <subject-uid>",
			short: "Record that a face is NOT this person (editor or admin)",
			long: "Record that a face is NOT this person, so the suggestion stays refused\n" +
				"instead of coming back on every sweep. Nothing is detached: this is an\n" +
				"opinion about a guess, not an edit of the photo.",
			past: "rejected as",
			pick: func(c *ctl.Client) faceOpinion { return c.RejectFace },
		},
		{
			use:   "unreject <photo-uid> <face> <subject-uid>",
			short: "Withdraw a rejection (editor or admin)",
			long:  "Withdraw a rejection, letting the person be suggested for this face again.",
			past:  "no longer rejected as",
			pick:  func(c *ctl.Client) faceOpinion { return c.UnrejectFace },
		},
		{
			use:   "confirm <photo-uid> <face> <subject-uid>",
			short: "Record that a face really IS this person (editor or admin)",
			long: "Record that a face really IS this person — the opposite of a rejection, not\n" +
				"a stronger form of it. A confirmed face drops out of that person's outlier\n" +
				"review, so the same false alarm is not raised over and over.",
			past: "confirmed as",
			pick: func(c *ctl.Client) faceOpinion { return c.ConfirmFace },
		},
		{
			use:   "unconfirm <photo-uid> <face> <subject-uid>",
			short: "Withdraw a confirmation (editor or admin)",
			long: "Withdraw a confirmation, letting the assignment be questioned by the\n" +
				"outlier review again.",
			past: "no longer confirmed as",
			pick: func(c *ctl.Client) faceOpinion { return c.UnconfirmFace },
		},
	}
}

// newCtlFaceOpinionCmd builds one of the four feedback commands. All four take the
// same three arguments, answer 204, and are idempotent.
func newCtlFaceOpinionCmd(opts *ctlOptions, spec faceOpinionSpec) *cobra.Command {
	return &cobra.Command{
		Use:   spec.use,
		Short: spec.short,
		Long:  spec.long,
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			faceIndex, err := parseFaceIndex(args[1])
			if err != nil {
				return err
			}
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			opinion := ctl.FaceOpinion{PhotoUID: args[0], FaceIndex: faceIndex, SubjectUID: args[2]}
			// Resolve the name before writing: the endpoint gives nothing back, and
			// a confirmation line naming only a uid cannot be checked by a reader.
			who, err := client.DescribeSubject(cmd.Context(), opinion.SubjectUID)
			if err != nil {
				return fmt.Errorf("fetching subject %s: %w", opinion.SubjectUID, err)
			}
			if err := spec.pick(client)(cmd.Context(), opinion); err != nil {
				return fmt.Errorf("recording the opinion about face %d of photo %s: %w",
					faceIndex, opinion.PhotoUID, err)
			}
			return renderAck(cmd.OutOrStdout(), out, fmt.Sprintf("face %d of photo %s %s %s",
				faceIndex, opinion.PhotoUID, spec.past, who))
		},
	}
}
