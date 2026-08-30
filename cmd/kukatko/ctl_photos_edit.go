package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/panbotka/kukatko/internal/ctl"
)

// photoEditFlags is the raw flag state of "ctl photos edit", before it is
// resolved against pflag's Changed into the request body.
//
// Every field here is a value the flag *could* hold; whether it is sent is
// decided by resolve, which asks pflag whether the operator actually wrote the
// flag. That is the whole point of the command: a photo's taken_at carries its
// own provenance, and re-sending the value that is already there would flip that
// provenance from "read out of the file" to "a person decided this".
type photoEditFlags struct {
	title       string
	description string
	notes       string
	aiNote      string

	subject   string
	keywords  string
	artist    string
	copyright string
	license   string
	scan      bool

	takenAt          string
	clearTakenAt     bool
	takenAtEstimated bool
	takenAtNote      string

	lat             float64
	lng             float64
	clearLocation   bool
	acceptEstimated bool
}

// editableFields maps each simple text flag to the JSON key it writes. They are
// listed together because they share one rule — send the string as given,
// including an empty one, which is how a NOT NULL text column is emptied.
func (f *photoEditFlags) editableFields() []struct {
	flag  string
	field string
	value *string
} {
	return []struct {
		flag  string
		field string
		value *string
	}{
		{"title", "title", &f.title},
		{"description", "description", &f.description},
		{"notes", "notes", &f.notes},
		{"ai-note", "ai_note", &f.aiNote},
		{"subject", "subject", &f.subject},
		{"keywords", "keywords", &f.keywords},
		{"artist", "artist", &f.artist},
		{"copyright", "copyright", &f.copyright},
		{"license", "license", &f.license},
		{"taken-at-note", "taken_at_note", &f.takenAtNote},
	}
}

// register declares every edit flag. The names mirror the API's own field names,
// so `-o json` output and the flag that produced it read the same.
//
// The fields the API serves but refuses to edit — software, color_profile,
// image_codec, camera_serial, original_name, projection — deliberately get no
// flags: they describe the file, not anybody's view of it, and offering a flag
// the server would reject is worse than offering none.
func (f *photoEditFlags) register(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVar(&f.title, "title", "", `set the title (--title "" empties it)`)
	flags.StringVar(&f.description, "description", "", "set the description")
	flags.StringVar(&f.notes, "notes", "", "set the private notes")
	flags.StringVar(&f.aiNote, "ai-note", "", "set the AI note")

	flags.StringVar(&f.subject, "subject", "", "set the IPTC subject/headline")
	flags.StringVar(&f.keywords, "keywords", "", "set the IPTC keywords (comma-separated, kept verbatim)")
	flags.StringVar(&f.artist, "artist", "", "set the IPTC artist")
	flags.StringVar(&f.copyright, "copyright", "", "set the copyright notice")
	flags.StringVar(&f.license, "license", "", "set the licence text")
	flags.BoolVar(&f.scan, "scan", false, "mark the photo as a scan of a print (--scan=false unmarks it)")

	flags.StringVar(&f.takenAt, "taken-at", "",
		"set the capture time: a date, or a date and time, or an RFC 3339 timestamp")
	flags.BoolVar(&f.clearTakenAt, "clear-taken-at", false, "remove the capture time, leaving the photo undated")
	flags.BoolVar(&f.takenAtEstimated, "taken-at-estimated", false,
		"mark the date as a guess (--taken-at-estimated=false marks it a fact and drops the note)")
	flags.StringVar(&f.takenAtNote, "taken-at-note", "",
		"what the estimated date rests on, in your own words")

	flags.Float64Var(&f.lat, "lat", 0, "set the latitude (needs --lng)")
	flags.Float64Var(&f.lng, "lng", 0, "set the longitude (needs --lat)")
	flags.BoolVar(&f.clearLocation, "clear-location", false, "remove the position")
	flags.BoolVar(&f.acceptEstimated, "accept-location", false,
		"accept the estimated position as your own decision (location_source=manual)")
}

// resolve turns the flag state into the request body, sending only the fields the
// operator actually wrote.
//
// Omission and clearing are two different requests and stay two different flags:
// a text field is emptied by passing it the empty string, and the three nullable
// ones (taken_at, lat, lng) by their own --clear-* flag, which sends an explicit
// JSON null. Nothing here re-implements a server rule — the length caps, the
// dropping of taken_at_note when the estimate flag goes, and which
// location_source a client may claim all stay on the server, and ctl reports the
// 400 it gets back.
func (f *photoEditFlags) resolve(flags *pflag.FlagSet) (*ctl.PhotoEdit, error) {
	edit := &ctl.PhotoEdit{}
	for _, field := range f.editableFields() {
		if flags.Changed(field.flag) {
			edit.Set(field.field, *field.value)
		}
	}
	if flags.Changed("scan") {
		edit.Set("scan", f.scan)
	}
	if flags.Changed("taken-at-estimated") {
		edit.Set("taken_at_estimated", f.takenAtEstimated)
	}
	if err := f.resolveTakenAt(flags, edit); err != nil {
		return nil, err
	}
	if err := f.resolveLocation(flags, edit); err != nil {
		return nil, err
	}
	return edit, nil
}

// resolveTakenAt writes the capture time, refusing to both set and clear it.
func (f *photoEditFlags) resolveTakenAt(flags *pflag.FlagSet, edit *ctl.PhotoEdit) error {
	setting := flags.Changed("taken-at")
	if setting && f.clearTakenAt {
		return fmt.Errorf("%w: --taken-at and --clear-taken-at", ctl.ErrConflictingEdits)
	}
	if f.clearTakenAt {
		edit.Clear("taken_at")
		return nil
	}
	if !setting {
		return nil
	}
	takenAt, err := ctl.ParseTakenAt(f.takenAt)
	if err != nil {
		return fmt.Errorf("reading --taken-at: %w", err)
	}
	edit.SetTime("taken_at", takenAt)
	return nil
}

// resolveLocation writes the coordinates, refusing a half pair, a set-and-clear
// pair, and accepting a position in the same breath as removing one.
func (f *photoEditFlags) resolveLocation(flags *pflag.FlagSet, edit *ctl.PhotoEdit) error {
	lat, lng := flags.Changed("lat"), flags.Changed("lng")
	switch {
	case lat != lng:
		return ctl.ErrIncompleteLocation
	case (lat || lng) && f.clearLocation:
		return fmt.Errorf("%w: --lat/--lng and --clear-location", ctl.ErrConflictingEdits)
	case f.clearLocation && f.acceptEstimated:
		return fmt.Errorf("%w: --clear-location and --accept-location", ctl.ErrConflictingEdits)
	}
	switch {
	case f.clearLocation:
		edit.Clear("lat")
		edit.Clear("lng")
	case lat:
		edit.Set("lat", f.lat)
		edit.Set("lng", f.lng)
	}
	if f.acceptEstimated {
		edit.Set("location_source", ctl.LocationSourceManual)
	}
	return nil
}

// newCtlPhotosEditCmd builds "ctl photos edit <uid>", the whole editable surface
// of PATCH /photos/{uid} as flags.
func newCtlPhotosEditCmd(opts *ctlOptions) *cobra.Command {
	var (
		flags  photoEditFlags
		detail ctl.PhotoDetailOptions
		dryRun bool
	)
	cmd := &cobra.Command{
		Use:   "edit <uid>",
		Short: "Edit one photo's metadata (editor or admin)",
		Long: "Edit one photo's metadata.\n\n" +
			"Only the flags you actually write are sent. That is not an optimisation: a\n" +
			"photo's capture time carries its own provenance, and re-sending the value that\n" +
			"is already there would flip it from \"read out of the file\" to \"a person decided\n" +
			"this\". Emptying a field is therefore its own act — pass \"\" to a text flag, or\n" +
			"--clear-taken-at / --clear-location for the three nullable ones.\n\n" +
			"The rules stay on the server: the length caps, the dating note that only lives\n" +
			"while the date is flagged as an estimate, and which location_source a client may\n" +
			"claim. ctl reports what the server says.\n\n" +
			"--dry-run prints the request body and writes nothing. This runs against a live\n" +
			"family archive; show your intent before you change it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			edit, err := flags.resolve(cmd.Flags())
			if err != nil {
				return err
			}
			if err := edit.Validate(); err != nil {
				return fmt.Errorf("checking the edit: %w", err)
			}
			if dryRun {
				return printEditBody(cmd, args[0], edit)
			}
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.EditPhoto(cmd.Context(), args[0], edit, detail)
			if err != nil {
				return fmt.Errorf("editing photo %s: %w", args[0], err)
			}
			return renderPhotoDetail(cmd.OutOrStdout(), out, raw)
		},
	}
	flags.register(cmd)
	local := cmd.Flags()
	local.BoolVar(&dryRun, "dry-run", false, "print the request body that would be sent and exit")
	local.BoolVar(&detail.People, "people", false, "also report who is on the photo in the refreshed detail")
	return cmd
}

// printEditBody writes what --dry-run promises: the exact JSON body the command
// would PATCH, and the photo it would go to. Nothing is contacted, so this works
// without a configured context too.
func printEditBody(cmd *cobra.Command, uid string, edit *ctl.PhotoEdit) error {
	body, err := edit.Body()
	if err != nil {
		return fmt.Errorf("rendering the edit: %w", err)
	}
	cmd.Printf("PATCH /api/v1/photos/%s\n%s\n", uid, body)
	return nil
}
