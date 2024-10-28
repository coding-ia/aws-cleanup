package cmd

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/spf13/cobra"
)

var region string
var owner string
var nameFilter string
var autoCleanup bool

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Cleanup AMI's",
	Run: func(cmd *cobra.Command, args []string) {

		cleanAMIs(cmd.Context())
	},
}

func init() {
	rootCmd.AddCommand(cleanCmd)

	cleanCmd.Flags().StringVarP(&region, "region", "r", "us-east-1", "AWS region to check for AMIs")
	cleanCmd.Flags().StringVarP(&owner, "owner", "o", "self", "AMI owner")
	cleanCmd.Flags().StringVarP(&nameFilter, "name", "n", "", "Name filter")
	cleanCmd.Flags().BoolVar(&autoCleanup, "auto-cleanup", false, "Automatically delete AMIs and associated snapshots")
}

func cleanAMIs(ctx context.Context) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		log.Fatalf("unable to load SDK config, %v", err)
	}

	svc := ec2.NewFromConfig(cfg)

	// Calculate the date 30 days ago
	cutoffDate := time.Now().AddDate(0, 0, -60)

	input := &ec2.DescribeImagesInput{
		Owners: []string{owner},
	}

	result, err := svc.DescribeImages(ctx, input)
	if err != nil {
		log.Fatalf("failed to describe images, %v", err)
	}

	var filteredAMIs []types.Image

	for _, image := range result.Images {
		createdAt, err := time.Parse(time.RFC3339, *image.CreationDate)
		if err != nil {
			log.Printf("Error parsing creation date for AMI %s: %v", *image.ImageId, err)
			continue
		}

		if createdAt.Before(cutoffDate) {
			filteredAMIs = append(filteredAMIs, image)
		}
	}

	if nameFilter != "" {
		filteredAMIs = filterAMIsByName(nameFilter, filteredAMIs)
	}

	fmt.Printf("Filtered AMIs:\n")
	for _, ami := range filteredAMIs {
		fmt.Printf("AMI ID: %s, Name: %s, Creation Date: %s\n",
			*ami.ImageId,
			aws.ToString(ami.Name),
			aws.ToString(ami.CreationDate))
	}

	if autoCleanup {
		//deleteAMIs(ctx, svc, filteredAMIs)
	}
}

func filterAMIsByName(filter string, images []types.Image) []types.Image {
	var filtered []types.Image

	for _, image := range images {
		if strings.Contains(strings.ToLower(aws.ToString(image.Name)), "test") {
			filtered = append(filtered, image)
		}
	}

	return filtered

}

func deleteAMIs(ctx context.Context, svc *ec2.Client, images []types.Image) {
	for _, image := range images {
		_, err := svc.DeregisterImage(ctx, &ec2.DeregisterImageInput{
			ImageId: image.ImageId,
		})
		if err != nil {
			log.Printf("Error deregistering AMI %s: %v\n", *image.ImageId, err)
			continue
		}
		fmt.Printf("Deregistered AMI: %s\n", *image.ImageId)

		for _, blockDevice := range image.BlockDeviceMappings {
			if blockDevice.Ebs != nil && blockDevice.Ebs.SnapshotId != nil {
				_, err := svc.DeleteSnapshot(ctx, &ec2.DeleteSnapshotInput{
					SnapshotId: blockDevice.Ebs.SnapshotId,
				})
				if err != nil {
					log.Printf("Error deleting snapshot %s for AMI %s: %v\n", *blockDevice.Ebs.SnapshotId, *image.ImageId, err)
					continue
				}
				fmt.Printf("Deleted Snapshot: %s for AMI: %s\n", *blockDevice.Ebs.SnapshotId, *image.ImageId)
			}
		}
	}
}
