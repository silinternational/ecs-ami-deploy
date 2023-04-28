package ead

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

func Test_isNewerImage(t *testing.T) {
	type args struct {
		first  ec2types.Image
		second ec2types.Image
	}
	tests := []struct {
		name    string
		args    args
		want    bool
		wantErr bool
	}{
		{
			name: "first is newer",
			args: args{
				first: ec2types.Image{
					CreationDate: aws.String("2020-05-01T00:00:00Z"),
				},
				second: ec2types.Image{
					CreationDate: aws.String("2020-04-01T00:00:00Z"),
				},
			},
			want:    false,
			wantErr: false,
		},
		{
			name: "second is newer",
			args: args{
				first: ec2types.Image{
					CreationDate: aws.String("2020-05-01T00:00:00Z"),
				},
				second: ec2types.Image{
					CreationDate: aws.String("2020-06-01T00:00:00Z"),
				},
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "parse error",
			args: args{
				first: ec2types.Image{
					CreationDate: aws.String("abc123"),
				},
				second: ec2types.Image{
					CreationDate: aws.String("not a date"),
				},
			},
			want:    false,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := isNewerImage(tt.args.first, tt.args.second)
			if (err != nil) != tt.wantErr {
				t.Errorf("isNewerImage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("isNewerImage() got = %v, want %v", got, tt.want)
			}
		})
	}
}

//func TestLatestAMIID(t *testing.T) {
//	awsCfg, err := config.LoadDefaultConfig(context.TODO())
//	if err != nil {
//		t.Error(err)
//		return
//	}
//	type args struct {
//		awsCfg aws.Config
//		filter string
//	}
//	tests := []struct {
//		name    string
//		args    args
//		want    string
//		wantErr bool
//	}{
//		{
//			name: "get specific ami id",
//			args: args{
//				awsCfg: awsCfg,
//				filter: "amzn2-ami-ecs-hvm-2.0.20200319-x86_64-ebs",
//			},
//			want:    "ami-00f69adbdc780866c",
//			wantErr: false,
//		},
//	}
//	for _, tt := range tests {
//		t.Run(tt.name, func(t *testing.T) {
//			got, err := LatestAMI(tt.args.awsCfg, tt.args.filter)
//			if (err != nil) != tt.wantErr {
//				t.Errorf("LatestAMI() error = %v, wantErr %v", err, tt.wantErr)
//				return
//			}
//			if got.ImageId != nil && *got.ImageId != tt.want {
//				t.Errorf("LatestAMI() got = %v, want %v", got, tt.want)
//			}
//		})
//	}
//}

func TestSortLT(t *testing.T) {
	now := time.Now()
	oneDay := 60 * 24 * time.Minute
	ltvs := []ec2types.LaunchTemplateVersion{
		{
			CreateTime: aws.Time(now.Add(-oneDay)),
		},
		{
			CreateTime: aws.Time(now.Add(-10 * oneDay)),
		},
		{
			CreateTime: aws.Time(now.Add(-20 * oneDay)),
		},
		{
			CreateTime: aws.Time(now.Add(-50 * oneDay)),
		},
		{
			CreateTime: aws.Time(now.Add(-5 * oneDay)),
		},
	}

	reverseSortLaunchTemplateVersions(ltvs)

	for i := range ltvs {
		// make sure not to test value out of range
		if i+1 >= len(ltvs) {
			continue
		}
		if ltvs[i].CreateTime.Before(*ltvs[i+1].CreateTime) {
			t.Errorf("time is not sorted as expected. index %v is %v, index %v+1 is %v",
				i, ltvs[i].CreateTime.Unix(), i, ltvs[i+1].CreateTime.Unix())
		}
	}

	//for _, i := range ltvs {
	//	log.Printf("%v - %s", i.CreatedTime.Unix(), i.CreatedTime.Format(time.RFC3339))
	//}
}
