package cmd

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/spf13/cobra"
	"log"
	"strings"
	"time"
)

type SnapshotOptions struct {
	AssumeRole  string
	Region      string
	Owner       string
	Description string
	Age         int
	AutoCleanup bool
}

var snapshotOptions = SnapshotOptions{}

var snapshotCmd = &cobra.Command{
	Use:   "snapshot",
	Short: "Snapshot cleanup",
	Run: func(cmd *cobra.Command, args []string) {
		cleanSnapshots(cmd.Context())
	},
}

func init() {
	rootCmd.AddCommand(snapshotCmd)

	snapshotCmd.Flags().StringVar(&snapshotOptions.AssumeRole, "assume-role", "", "Role to assume")
	snapshotCmd.Flags().StringVarP(&snapshotOptions.Region, "region", "r", "us-east-1", "AWS region to check for AMIs")
	snapshotCmd.Flags().StringVarP(&snapshotOptions.Owner, "owner", "o", "self", "Snapshot owner")
	snapshotCmd.Flags().StringVarP(&snapshotOptions.Description, "description", "d", "", "Description filter")
	snapshotCmd.Flags().IntVarP(&snapshotOptions.Age, "age", "a", 30, "Max age")
	snapshotCmd.Flags().BoolVar(&snapshotOptions.AutoCleanup, "auto-cleanup", false, "Automatically delete snapshots")
}

func cleanSnapshots(ctx context.Context) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(snapshotOptions.Region))
	if err != nil {
		log.Fatalf("unable to load SDK config, %v", err)
	}

	if snapshotOptions.AssumeRole != "" {
		fmt.Printf("Assuming role: %s\n", snapshotOptions.AssumeRole)
		cfg.Credentials = assumeRole(ctx, cfg, snapshotOptions.AssumeRole)
	}

	svc := ec2.NewFromConfig(cfg)

	if snapshotOptions.Age <= 0 {
		log.Fatalf("age cannot be less than or equal to zero")
	}

	cutoffDate := time.Now().AddDate(0, 0, -snapshotOptions.Age)

	input := &ec2.DescribeSnapshotsInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("status"),
				Values: []string{"completed"},
			},
		},
		OwnerIds: []string{snapshotOptions.Owner},
	}

	snapshots, err := svc.DescribeSnapshots(ctx, input)
	if err != nil {
		log.Fatalf("failed to describe snapshots, %v", err)
	}

	var filteredSnapshots []types.Snapshot

	for _, snapshot := range snapshots.Snapshots {
		if snapshot.StartTime.Before(cutoffDate) {
			filteredSnapshots = append(filteredSnapshots, snapshot)
		}
	}

	if snapshotOptions.Description != "" {
		filteredSnapshots = filterSnapshotsByDescription(snapshotOptions.Description, filteredSnapshots)
	}

	if filteredSnapshots != nil {
		fmt.Printf("Filtered Snapshots:\n")
		for _, snapshot := range filteredSnapshots {
			fmt.Printf("Snapshot ID: %s, Description: %s, Start Time: %s\n",
				*snapshot.SnapshotId,
				*snapshot.Description,
				snapshot.StartTime)
		}

		if snapshotOptions.AutoCleanup {
			deleteSnapshots(ctx, svc, filteredSnapshots)
		}
	} else {
		fmt.Println("No snapshots found.")
	}
}

func filterSnapshotsByDescription(filter string, snapshots []types.Snapshot) []types.Snapshot {
	var filtered []types.Snapshot

	for _, snapshot := range snapshots {
		if strings.Contains(aws.ToString(snapshot.Description), filter) {
			filtered = append(filtered, snapshot)
		}
	}

	return filtered

}

func deleteSnapshots(ctx context.Context, svc *ec2.Client, snapshots []types.Snapshot) {
	for _, snapshot := range snapshots {
		input := &ec2.DeleteSnapshotInput{
			SnapshotId: snapshot.SnapshotId,
		}

		_, err := svc.DeleteSnapshot(ctx, input)
		if err != nil {
			log.Printf("Could not delete snapshot %s: %v", *snapshot.SnapshotId, err)
		} else {
			fmt.Printf("Successfully deleted snapshot: %s\n", *snapshot.SnapshotId)
		}
	}
}
