package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// newCtlClustersCmd builds the "ctl clusters" tree: the groups of unassigned faces
// the auto-clustering found, served by internal/clusterapi. Naming a whole cluster
// is the cheapest curation there is — one command names a person on every photo
// the clustering put in that group — which is also why removing a stray face
// first matters.
func newCtlClustersCmd(opts *ctlOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clusters",
		Short: "List clusters of unnamed faces, name a whole one, or drop a stray face",
	}
	cmd.AddCommand(newCtlClustersListCmd(opts), newCtlClustersAssignCmd(opts),
		newCtlClustersRemoveFaceCmd(opts))
	return cmd
}

// newCtlClustersListCmd builds "ctl clusters list", one page of the cluster
// listing. It is paginated: the server serves a bounded page and says where the
// next one starts, because a real library holds far more groups than anybody
// reads in one sitting.
func newCtlClustersListCmd(opts *ctlOptions) *cobra.Command {
	var limit, offset int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List a page of the clusters of unnamed faces with their suggested identity",
		Long: "List the clusters of unnamed faces, a page at a time.\n\n" +
			"SUGGESTION is the nearest already-named subject with the cosine distance that\n" +
			"ranked it — a guess, never an assignment. REPRESENTATIVE is one face of the\n" +
			"group as `<photo-uid> #<face-index>`, which is what `ctl photos image` and\n" +
			"`ctl clusters remove-face` take.\n\n" +
			"Only groups the server has already prepared are listed; the rest are counted\n" +
			"below the table and prepared in the background, which this command asks for.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.ListClusters(cmd.Context(), limit, offset)
			if err != nil {
				return fmt.Errorf("listing face clusters: %w", err)
			}
			return renderClusters(cmd.OutOrStdout(), out, raw)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "how many groups to list (server default when unset)")
	cmd.Flags().IntVar(&offset, "offset", 0, "where to start, as the previous page reported it")
	return cmd
}

// newCtlClustersAssignCmd builds "ctl clusters assign <cluster-uid> [<subject-uid>]",
// which names every face of the cluster at once and consumes it.
func newCtlClustersAssignCmd(opts *ctlOptions) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "assign <cluster-uid> [<subject-uid>]",
		Short: "Assign every face of a cluster to one person (editor or admin)",
		Long: "Assign every face of a cluster to one person, in one action.\n\n" +
			"Name the person by uid, or with --name to have the server find the person of\n" +
			"that name and create them if the library has never heard of them.\n\n" +
			"The cluster is consumed: its faces become that person's markers and the group\n" +
			"is gone. Look at it first (`ctl clusters list`, then `ctl photos image`), and\n" +
			"drop the faces that do not belong with `ctl clusters remove-face`.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			subject, err := ctl.SubjectRefFromArgs(positional(args, 1), name)
			if err != nil {
				return fmt.Errorf("reading the subject: %w", err)
			}
			raw, err := client.AssignCluster(cmd.Context(), args[0], subject)
			if err != nil {
				return fmt.Errorf("assigning cluster %s to %s: %w", args[0], subject, err)
			}
			return renderClusterAssign(cmd.OutOrStdout(), out, raw)
		},
	}
	cmd.Flags().StringVar(&name, "name", "",
		"name the person instead of naming their uid; an unknown name creates the subject")
	return cmd
}

// newCtlClustersRemoveFaceCmd builds
// "ctl clusters remove-face <cluster-uid> <photo-uid> <face>", the repair for a
// group the clustering put one wrong face into.
func newCtlClustersRemoveFaceCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "remove-face <cluster-uid> <photo-uid> <face>",
		Short: "Drop one face that does not belong in a cluster (editor or admin)",
		Long: "Drop one face that does not belong in a cluster, before the rest is named.\n\n" +
			"The face goes back to being unassigned; nothing is deleted and no marker is\n" +
			"written. When the face was the last one, the cluster itself is removed and the\n" +
			"confirmation says so.",
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			faceIndex, err := parseFaceIndex(args[2])
			if err != nil {
				return err
			}
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.RemoveClusterFace(cmd.Context(), args[0], args[1], faceIndex)
			if err != nil {
				return fmt.Errorf("removing face %d of photo %s from cluster %s: %w",
					faceIndex, args[1], args[0], err)
			}
			return renderClusterRemoval(cmd.OutOrStdout(), out, raw, args[0])
		},
	}
}
