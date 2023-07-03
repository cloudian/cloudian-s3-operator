/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"encoding/json"
	"fmt"
	_ "net/url"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	awsuser "github.com/aws/aws-sdk-go/service/iam"
	"github.com/golang/glog"
	apibkt "github.com/kube-object-storage/lib-bucket-provisioner/pkg/provisioner/api"
	storageV1 "k8s.io/api/storage/v1"
)

// PolicyDocument is the structure of IAM policy document
type PolicyDocument struct {
	Version   string
	Statement []StatementEntry
}

// StatementEntry is used to define permission statements in a PolicyDocument
type StatementEntry struct {
	Sid      string
	Effect   string
	Action   []string
	Resource []string `json:",omitempty"`
}

// handleUserAndPolicy takes care of policy and user creation when flag is set.
func (p *awsS3Provisioner) handleUserAndPolicy(bktName string, options *apibkt.BucketOptions) (userAccessId, userSecretKey string, err error) {

	glog.V(2).Infof("creating user and policy for bucket %q", bktName)

	// Create the user
	uname := p.bktUserName
	_, err = p.iamsvc.CreateUser(&awsuser.CreateUserInput{
		UserName: &uname,
	})
	if err != nil {
		//should we fail here or keep going?
		glog.Errorf("error creating IAM user %q: %v", uname, err)
		return
	}
	// If something goes wrong after this point delete the IAM user
	defer func() {
		if err != nil {
			_, delerr := p.iamsvc.DeleteUser(&awsuser.DeleteUserInput{UserName: &uname})
			if delerr != nil {
				glog.Errorf("Failed to undo creating IAM user %s: %v", uname, err)
			}
		}
	}()

	// Create an access key
	userAccessId, userSecretKey, err = p.createAccessKey(uname)
	if err != nil {
		//should we fail here or keep going?
		glog.Errorf("error creating IAM user %q: %v", uname, err)
		return
	}
	// If something goes wrong after this point delete the IAM user
	defer func() {
		if err != nil {
			_, delerr := p.iamsvc.DeleteAccessKey(&awsuser.DeleteAccessKeyInput{UserName: &uname, AccessKeyId: &userAccessId})
			if delerr != nil {
				glog.Errorf("Failed to undo creating IAM user %s: %v", uname, err)
			}
		}
	}()

	//Create the Policy for the user + bucket
	//if createBucket was successful
	//might change the input param into this function, we need bucketName
	//and maybe accessPerms (read, write, read/write)
	policyDoc, err := p.createBucketPolicyDocument(bktName, options)
	if err != nil {
		//We did get our user created, but not our policy doc
		//I'm going to pass back our user for now
		glog.Errorf("error creating policyDoc %s: %v", bktName, err)
		return
	}

	// Create the policy in aws for the user and bucket
	// policyName is same as username
	_, err = p.createUserPolicy(p.iamsvc, uname, policyDoc)
	if err != nil {
		//should we fail here or keep going?
		glog.Errorf("error creating userPolicy for user %q on bucket %q: %v", uname, bktName, err)
		return
	}
	// If something goes wrong after this point then delete policy document
	defer func() {
		if err != nil {
			_, delerr := p.iamsvc.DeletePolicy(&awsuser.DeletePolicyInput{PolicyArn: aws.String(uname)})
			if delerr != nil {
				glog.Errorf("Failed to undo creating IAM policy %s: %v", uname, err)
			}
		}
	}()

	//attach policy to user - policyName and username are same
	err = p.attachPolicyToUser(uname)
	if err != nil {
		glog.Errorf("error attaching userPolicy for user %q on bucket %q: %v", uname, bktName, err)
		return
	}

	glog.V(2).Infof("successfully created user and policy for bucket %q", bktName)
	return
}

func (p *awsS3Provisioner) handleUserAndPolicyDeletion(bktName string) error {

	if p.bktCreateUser != "yes" {
		return nil
	}

	glog.V(2).Infof("deleting user and policy for bucket %q", bktName)

	uname := p.bktUserName
	p.iamsvc = awsuser.New(p.iamSession)
	arn := p.bktUserPolicyArn

	// Detach Policy
	_, err := p.iamsvc.DetachUserPolicy((&awsuser.DetachUserPolicyInput{PolicyArn: aws.String(arn), UserName: aws.String(uname)}))
	if err != nil && !isNoSuchEntityError(err) {
		glog.Errorf("Error detaching User Policy %s %v", arn, err)
		return err
	}
	glog.V(2).Infof("successfully detached policy %q, user %q", arn, uname)

	// Delete Policy
	_, err = p.iamsvc.DeletePolicy(&awsuser.DeletePolicyInput{PolicyArn: aws.String(arn)})
	if err != nil && !isNoSuchEntityError(err) {
		glog.Errorf("Error deleting User Policy %s %v", arn, err)
		return err
	}
	glog.V(2).Infof("successfully deleted policy %q", arn)

	// Delete AccessKeys
	// TODO: error handling
	accessKeyId, _ := p.getAccessKey(uname)
	if len(accessKeyId) != 0 {
		_, err = p.iamsvc.DeleteAccessKey(&awsuser.DeleteAccessKeyInput{AccessKeyId: aws.String(accessKeyId), UserName: aws.String(uname)})
		if err != nil && !isNoSuchEntityError(err) {
			glog.Errorf("Error deleting access key for user %s %v", uname, err)
			return err
		}
		glog.V(2).Infof("successfully deleted access key for user %q", uname)
	}

	// Delete IAM User
	glog.V(2).Infof("Deleting User %q", uname)
	_, err = p.iamsvc.DeleteUser(&awsuser.DeleteUserInput{UserName: aws.String(uname)})
	if err != nil && !isNoSuchEntityError(err) {
		glog.Errorf("Error deleting User %s %v", uname, err)
		return err
	}

	glog.V(2).Infof("successfully deleted user and policy for bucket %q", bktName)
	return err
}

func (p *awsS3Provisioner) createBucketPolicyDocument(bktName string, options *apibkt.BucketOptions) (string, error) {

	arn := fmt.Sprintf(s3BucketArn, bktName)
	p.bktUserPolicyArn = arn
	glog.V(2).Infof("createBucketPolicyDocument for bucket %q and ARN %q", bktName, arn)

	read := StatementEntry{
		Sid:    "s3Read",
		Effect: "Allow",
		Action: []string{
			"s3:ListAllMyBuckets",
			"s3:HeadObject",
			"s3:ListBucket",
			"s3:GetBucketAcl",
			"s3:GetBucketCORS",
			"s3:GetBucketLocation",
			"s3:GetBucketLogging",
			"s3:GetBucketNotification",
			"s3:GetBucketObjectLockConfiguration",
			"s3:GetBucketPolicy",
			"s3:GetBucketRequestPayment",
			"s3:GetBucketTagging",
			"s3:GetBucketVersioning",
			"s3:GetBucketWebsite",
			"s3:GetEncryptionConfiguration",
			"s3:GetLifecycleConfiguration",
			"s3:GetObject",
			"s3:GetObjectAcl",
			"s3:GetObjectLegalHold",
			"s3:GetObjectRetention",
			"s3:GetObjectTagging",
			"s3:GetObjectTorrent",
			"s3:GetObjectVersion",
			"s3:GetObjectVersionAcl",
			"s3:GetObjectVersionTagging",
			"s3:GetReplicationConfiguration",
			"s3:ListBucketMultipartUploads",
			"s3:ListBucketVersions",
			"s3:ListMultipartUploadParts"},
		Resource: []string{arn + "/*", arn},
	}
	write := StatementEntry{
		Sid:    "s3Write",
		Effect: "Allow",
		Action: []string{
			"s3:AbortMultipartUpload",
			"s3:CreateBucket",
			"s3:DeleteBucket",
			"s3:DeleteBucketWebsite",
			"s3:DeleteObject",
			"s3:DeleteObjectTagging",
			"s3:DeleteObjectVersion",
			"s3:DeleteObjectVersionTagging",
			"s3:PutBucketCORS",
			"s3:PutBucketLogging",
			"s3:PutBucketNotification",
			"s3:PutBucketObjectLockConfiguration",
			"s3:PutBucketRequestPayment",
			"s3:PutBucketTagging",
			"s3:PutBucketVersioning",
			"s3:PutBucketWebsite",
			"s3:PutEncryptionConfiguration",
			"s3:PutLifecycleConfiguration",
			"s3:PutObject",
			"s3:PutObjectLegalHold",
			"s3:PutObjectRetention",
			"s3:PutObjectTagging",
			"s3:PutObjectVersionTagging",
			"s3:PutReplicationConfiguration",
			"s3:RestoreObject",
		},
		Resource: []string{arn + "/*", arn},
	}

	policy := PolicyDocument{
		Version:   "2012-10-17",
		Statement: []StatementEntry{},
	}

	// Check if the storage class has provided a storage policy we should use...
	if p, ok := options.Parameters["iamPolicy"]; ok {
		err := json.Unmarshal([]byte(p), &policy)
		if err != nil {
			return "", err
		}
		// Ensure each policy is tied to just this bucket
		for idx := range policy.Statement {
			policy.Statement[idx].Resource = []string{arn + "/*", arn}
		}
	} else {
		// do a switch case here to figure out which policy to include
		// for now we are commenting until we can update the lib
		// this will come from bucketOptions I'm guessing (obc or sc params)?
		/*
			if spec.LocalPermission != nil {
				switch *spec.LocalPermission {
				case storageV1.ReadOnlyPermission:
					policy.Statement = append(policy.Statement, read)
				case storageV1.WriteOnlyPermission:
					policy.Statement = append(policy.Statement, write)
				case storageV1.ReadWritePermission:
					policy.Statement = append(policy.Statement, read, write)
				default:
					return "", fmt.Errorf("unknown permission, %s", *spec.LocalPermission)
				}
			}
		*/
		policy.Statement = append(policy.Statement, read, write)
	}

	b, err := json.MarshalIndent(&policy, "", "  ")
	if err != nil {
		return "", fmt.Errorf("error marshaling policy, %s", err.Error())
	}

	return string(b), nil
}

func (p awsS3Provisioner) createUserPolicy(iamsvc *awsuser.IAM, policyName string, policyDocument string) (*awsuser.CreatePolicyOutput, error) {

	policyInput := &awsuser.CreatePolicyInput{
		PolicyName:     aws.String(policyName),
		PolicyDocument: aws.String(policyDocument),
	}

	result, err := iamsvc.CreatePolicy(policyInput)
	if err != nil {
		fmt.Println("Error", err)
		return nil, err
	}

	glog.V(2).Infof("createUserPolicy %q successfully created", policyName)
	return result, nil
}

func (p *awsS3Provisioner) getPolicyARN(policyName string) (string, error) {

	glog.V(2).Infof("getting ARN for policy %q", policyName)
	accountID, err := p.getAccountID()
	if err != nil {
		return "", err
	}

	// set the accountID in our provisioner
	p.bktUserAccountId = accountID
	policyARN := fmt.Sprintf(policyArn, accountID, policyName)
	// set the policyARN for our provisioner
	p.bktUserPolicyArn = policyARN
	glog.V(2).Infof("successfully got PolicyARN %q for AccountID %s's Policy %q", policyARN, accountID, policyName)

	return policyARN, nil
}

func (p *awsS3Provisioner) attachPolicyToUser(policyName string) error {

	glog.V(2).Infof("attach policy %q to user", policyName)
	policyARN, err := p.getPolicyARN(policyName)
	if err != nil {
		return err
	}

	_, err = p.iamsvc.AttachUserPolicy(&awsuser.AttachUserPolicyInput{PolicyArn: aws.String(policyARN), UserName: aws.String(p.bktUserName)})
	if err != nil {
		return err
	}

	glog.V(2).Infof("successfully attached policy %q to user %q", policyName, p.bktUserName)
	return err
}

// getAccountID - Gets the accountID of the authenticated session.
func (p *awsS3Provisioner) getAccountID() (string, error) {

	glog.V(2).Infof("creating new user %q", p.bktUserName)
	user, err := p.iamsvc.GetUser(&awsuser.GetUserInput{
		UserName: &p.bktUserName})
	if err != nil {
		glog.Errorf("Could not get new user %s", p.bktUserName)
		return "", err
	}

	arnData, err := arn.Parse(*user.User.Arn)
	if err != nil {
		return "", err
	}

	glog.V(2).Infof("created user %q and accountID %q", p.bktUserName, aws.StringValue(&arnData.AccountID))
	return aws.StringValue(&arnData.AccountID), nil
}

func (p *awsS3Provisioner) createAccessKey(user string) (string, string, error) {
	// create the Access Keys for the new user
	aresult, err := p.iamsvc.CreateAccessKey(&awsuser.CreateAccessKeyInput{
		UserName: &user,
	})
	if err != nil {
		return "", "", err
	}

	// populate our receiver
	acccessId := aws.StringValue(aresult.AccessKey.AccessKeyId)
	secretKey := aws.StringValue(aresult.AccessKey.SecretAccessKey)

	return acccessId, secretKey, nil
}

// getAccessKeyId - Gets the accountID of the authenticated session.
func (p *awsS3Provisioner) getAccessKey(username string) (string, error) {

	glog.V(2).Infof("getting access key for user %q", username)
	keys, err := p.iamsvc.ListAccessKeys(&awsuser.ListAccessKeysInput{UserName: aws.String(username)})
	if err != nil {
		glog.Errorf("Could not get access key for new user %s", username)
		return "", err
	}

	for _, keys := range keys.AccessKeyMetadata {
		return aws.StringValue(keys.AccessKeyId), nil
	}

	glog.V(2).Infof("no access key found for user %q", username)
	return "", nil
}

// check storage class params for createBucketUser and set
// provisioner receiver field.
func (p *awsS3Provisioner) setCreateBucketUserOptions(sc *storageV1.StorageClass) {

	const scBucketUser = "createBucketUser"

	// get sc user-access flag parameter
	newUser, ok := sc.Parameters[scBucketUser]
	if ok && newUser == "no" {
		glog.V(2).Infof("storage class flag %q indicates to NOT create a new user", scBucketUser)
		p.bktCreateUser = "no"
		return
	}

	glog.V(2).Infof("storage class flag %s's value, or absence of flag, indicates to create a new user", scBucketUser)
	p.bktCreateUser = "yes"
}
