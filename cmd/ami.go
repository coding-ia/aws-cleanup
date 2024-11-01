package cmd

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/spf13/cobra"
)

type AMIOptions struct {
	AssumeRole  string
	Region      string
	Owner       string
	NameFilter  string
	Age         int
	AutoCleanup bool
	Deprecated  bool
}

var amiOptions = AMIOptions{}

var cleanCmd = &cobra.Command{
	Use:   "ami",
	Short: "AMI cleanup",
	Run: func(cmd *cobra.Command, args []string) {
		cleanAMIs(cmd.Context())
	},
}

func init() {
	rootCmd.AddCommand(cleanCmd)

	cleanCmd.Flags().StringVar(&amiOptions.AssumeRole, "assume-role", "", "Role to assume")
	cleanCmd.Flags().StringVarP(&amiOptions.Region, "region", "r", "us-east-1", "AWS region to check for AMIs")
	cleanCmd.Flags().StringVarP(&amiOptions.Owner, "owner", "o", "self", "AMI owner")
	cleanCmd.Flags().StringVarP(&amiOptions.NameFilter, "name", "n", "", "Name filter")
	cleanCmd.Flags().IntVarP(&amiOptions.Age, "age", "a", 30, "Max age")
	cleanCmd.Flags().BoolVar(&amiOptions.AutoCleanup, "auto-cleanup", false, "Automatically delete AMIs and associated snapshots")
	cleanCmd.Flags().BoolVar(&amiOptions.Deprecated, "deprecated", false, "Select images that are deprecated")
}

func cleanAMIs(ctx context.Context) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(amiOptions.Region))
	if err != nil {
		log.Fatalf("unable to load SDK config, %v", err)
	}

	if amiOptions.AssumeRole != "" {
		fmt.Printf("Assuming role: %s\n", amiOptions.AssumeRole)
		cfg.Credentials = assumeRole(ctx, cfg, amiOptions.AssumeRole)
	}

	svc := ec2.NewFromConfig(cfg)

	if amiOptions.Age <= 0 {
		log.Fatalf("age cannot be less than or equal to zero")
	}

	cutoffDate := time.Now().AddDate(0, 0, -amiOptions.Age)

	input := &ec2.DescribeImagesInput{
		Owners: []string{amiOptions.Owner},
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
			if !amiOptions.Deprecated {
				filteredAMIs = append(filteredAMIs, image)
			} else {
				if image.DeprecationTime != nil {
					deprecateAt, _ := time.Parse(time.RFC3339, *image.DeprecationTime)
					if deprecateAt.Before(time.Now()) {
						filteredAMIs = append(filteredAMIs, image)
					}
				}
			}
		}
	}

	if amiOptions.NameFilter != "" {
		filteredAMIs = filterAMIsByName(amiOptions.NameFilter, filteredAMIs)
	}

	fmt.Printf("Filtered AMIs:\n")
	for _, ami := range filteredAMIs {
		fmt.Printf("AMI ID: %s, Name: %s, Creation Date: %s\n",
			*ami.ImageId,
			aws.ToString(ami.Name),
			aws.ToString(ami.CreationDate))
	}

	if amiOptions.AutoCleanup {
		deleteAMIs(ctx, svc, filteredAMIs)
	}
}

func filterAMIsByName(filter string, images []types.Image) []types.Image {
	var filtered []types.Image

	for _, image := range images {
		if strings.Contains(aws.ToString(image.Name), filter) {
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

func assumeRole(ctx context.Context, cfg aws.Config, roleArn string) aws.CredentialsProvider {
	stsClient := sts.NewFromConfig(cfg)
	creds := stscreds.NewAssumeRoleProvider(stsClient, roleArn, func(opts *stscreds.AssumeRoleOptions) {
		opts.RoleSessionName = "aws-sdk-go-cli-session"
		opts.Duration = time.Hour
	})
	return creds
}
