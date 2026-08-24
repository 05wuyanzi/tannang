// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, you can obtain one at http://mozilla.org/MPL/2.0/.

package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"time"

	"github.com/05wuyanzi/tannang/internal/buildinfo"
	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/evidence"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/fingerprint"
	"github.com/05wuyanzi/tannang/internal/integrity"
	"github.com/05wuyanzi/tannang/internal/receipt"
	"github.com/05wuyanzi/tannang/internal/resolver"
)

type firstStagePackageSession interface {
	OpenArtifact() (io.Writer, error)
	SealArtifact(retain bool) error
	HashArtifact() (integrity.Entry, error)
	Finalize(context.Context, receipt.FirstStagePackageMetadata, []receipt.FirstStageRecord) error
	Abort() error
}

type firstStagePackageFactory func(context.Context, string) (firstStagePackageSession, error)

func defaultFirstStagePackageFactory(ctx context.Context, output string) (firstStagePackageSession, error) {
	session, err := evidence.BeginFirstStagePackage(ctx, output)
	if session == nil {
		return nil, err
	}
	return session, err
}

func (s *FirstStage) runRealSelected(
	ctx context.Context,
	collection CollectionContext,
	target fingerprint.TargetFingerprint,
	record *CapabilityRecord,
	session *firstStagePackageSession,
	cancelled *bool,
) error {
	if ctx.Err() != nil {
		*cancelled = true
		s.markCancelled(record)
		return nil
	}
	if *session == nil {
		created, err := s.packageFactory(ctx, collection.OutputDestination)
		if isNilFirstStagePackageSession(created) {
			created = nil
		}
		*session = created
		if err == nil && created == nil {
			err = errors.New("first-stage package factory returned no session")
		}
		if err != nil {
			setPreAttemptPackageFailure(record)
			if created != nil {
				err = errors.Join(err, created.Abort())
			}
			return fmt.Errorf("begin first-stage package: %w", err)
		}
	}
	if ctx.Err() != nil {
		*cancelled = true
		s.markCancelled(record)
		return nil
	}
	sink, err := (*session).OpenArtifact()
	if err != nil {
		setPreAttemptPackageFailure(record)
		return fmt.Errorf("open first-stage artifact: %w", errors.Join(err, (*session).Abort()))
	}
	if ctx.Err() != nil {
		*cancelled = true
		s.markCancelled(record)
		if discardErr := (*session).SealArtifact(false); discardErr != nil {
			return fmt.Errorf("discard cancelled first-stage artifact: %w", errors.Join(discardErr, (*session).Abort()))
		}
		return nil
	}
	record.Attempted = true
	s.emitRuntime(RuntimeEvent{Type: RuntimeTypeActivity, Event: RuntimeEventProviderStarted})
	executionResult := s.streamingRunner.ExecuteTo(ctx, *record.Capability, target.Clone(), sink)
	s.emitRuntime(RuntimeEvent{Type: RuntimeTypeActivity, Event: RuntimeEventProviderFinished})
	if validationErr := executionResult.Validate(); validationErr != nil {
		executionResult = execution.Result{
			State:             execution.Failed,
			Reason:            execution.ReasonProviderError,
			Detail:            "Provider returned an invalid execution result: " + validationErr.Error(),
			SideEffectSummary: "Provider execution occurred, but its result failed contract validation.",
		}
	}
	record.Execution = cloneExecutionResult(executionResult)
	record.MissingEvidence = missingEvidenceFor(*record)
	retain := executionResult.State == execution.Collected || executionResult.State == execution.Partial
	if sealErr := (*session).SealArtifact(retain); sealErr != nil {
		return fmt.Errorf("finalize first-stage artifact sink: %w", errors.Join(sealErr, (*session).Abort()))
	}
	if retain {
		s.emitRuntime(RuntimeEvent{Type: RuntimeTypeActivity, Event: RuntimeEventArtifactSealed})
	}
	if ctx.Err() != nil && executionResult.State != execution.Collected {
		*cancelled = true
	}
	return nil
}

func setPreAttemptPackageFailure(record *CapabilityRecord) {
	record.Attempted = false
	record.Execution = unexecutedResult(execution.ReasonNone)
	record.OrchestrationReason = OrchestrationPackageFinalizationFailed
	record.MissingEvidence = []string{receipt.FirstStagePackagePreparationMissingEvidence}
	record.ArtifactReference = ""
	record.ReceiptReference = ""
}

func (s *FirstStage) finalizeReal(
	ctx context.Context,
	collection CollectionContext,
	target fingerprint.TargetFingerprint,
	result RunResult,
	session firstStagePackageSession,
	cancelled bool,
) (FinalizationResult, error) {
	if ctx == nil {
		return FinalizationResult{}, errors.New("real finalization context is required")
	}
	productVersion := buildinfo.Current().ProductVersion
	result.Records = cloneRecords(result.Records)
	if result.Fingerprint != nil {
		fingerprintCopy := result.Fingerprint.Clone()
		result.Fingerprint = &fingerprintCopy
	}
	if session == nil {
		created, err := s.packageFactory(ctx, collection.OutputDestination)
		if isNilFirstStagePackageSession(created) {
			created = nil
		}
		session = created
		if err == nil && created == nil {
			err = errors.New("first-stage package factory returned no receipt-only session")
		}
		if err != nil {
			if session != nil {
				err = errors.Join(err, session.Abort())
			}
			return FinalizationResult{}, fmt.Errorf("begin receipt-only first-stage package: %w", err)
		}
	}
	finishedAt := s.clock().UTC()
	if finishedAt.IsZero() {
		return FinalizationResult{}, errors.Join(errors.New("finalization clock returned zero time"), session.Abort())
	}
	finishedAtText := finishedAt.Format(time.RFC3339Nano)

	artifactRef := (*receipt.ArtifactReference)(nil)
	for index := range result.Records {
		if result.Records[index].Execution.State == execution.Collected || result.Records[index].Execution.State == execution.Partial {
			entry, err := session.HashArtifact()
			if err != nil {
				return FinalizationResult{}, fmt.Errorf("hash retained first-stage artifact: %w", errors.Join(err, session.Abort()))
			}
			candidate := receipt.ArtifactReference{
				Path:            entry.Path,
				MediaType:       receipt.FirstStageArtifactMedia,
				ContentSchemaID: receipt.FirstStageArtifactSchema,
				RawOrDerived:    "DERIVED",
				Size:            entry.Size,
				SHA256:          entry.SHA256,
			}
			if err := candidate.Validate(); err != nil {
				return FinalizationResult{}, errors.Join(err, session.Abort())
			}
			artifactRef = &candidate
			result.Records[index].ArtifactReference = candidate.Path
			break
		}
	}

	receipts := make([]receipt.FirstStageRecord, 0, len(result.Records))
	references := make([]FinalizationReference, 0, len(result.Records))
	receiptReferences := make([]string, 0, len(result.Records))
	for index := range result.Records {
		record := &result.Records[index]
		receiptPath := receipt.FirstStageReceiptPath(record.Request.ID)
		record.ReceiptReference = receiptPath
		var selected *receipt.ProviderIdentity
		if record.SelectedProvider != nil {
			selected = &receipt.ProviderIdentity{ID: record.SelectedProvider.ID, Class: record.SelectedProvider.Class}
		}
		var compatibilityReason *execution.Reason
		var evaluations []resolver.CandidateEvaluation
		if record.Decision != nil {
			reason := record.Decision.Reason
			compatibilityReason = &reason
			evaluations = append([]resolver.CandidateEvaluation(nil), record.Decision.Evaluations...)
		}
		var artifact *receipt.ArtifactReference
		if artifactRef != nil && record.ArtifactReference != "" {
			copy := *artifactRef
			artifact = &copy
		}
		startedAt := collection.StartedAt
		firstStageReceipt := receipt.FirstStageRecord{
			SchemaVersion:         receipt.SchemaVersion,
			ManifestVersion:       receipt.ManifestVersion,
			ProductVersion:        productVersion,
			RuntimeArtifact:       receipt.FirstStageRuntimeArtifact,
			CollectionID:          collection.CollectionID,
			CaseID:                collection.CaseID,
			TargetFingerprint:     target.Clone(),
			RequestedCapability:   record.Request,
			Capability:            cloneCapability(record.Capability),
			SelectedProvider:      selected,
			Compatibility:         record.Compatibility,
			CompatibilityReason:   compatibilityReason,
			CandidateEvaluations:  evaluations,
			Attempted:             record.Attempted,
			Execution:             cloneExecutionResult(record.Execution),
			OrchestrationReason:   string(record.OrchestrationReason),
			MissingEvidence:       append([]string(nil), record.MissingEvidence...),
			ArtifactReference:     artifact,
			AcquisitionStartedAt:  startedAt,
			AcquisitionFinishedAt: finishedAtText,
		}
		if err := firstStageReceipt.Validate(); err != nil {
			return FinalizationResult{}, fmt.Errorf("validate first-stage receipt %s: %w", record.Request.ID, errors.Join(err, session.Abort()))
		}
		receipts = append(receipts, firstStageReceipt)
		receiptReferences = append(receiptReferences, receiptPath)
		references = append(references, FinalizationReference{CapabilityID: record.Request.ID, ReceiptReference: receiptPath, ArtifactReference: record.ArtifactReference})
	}

	prospective := result
	prospective.Records = cloneRecords(result.Records)
	if result.Fingerprint != nil {
		fingerprintCopy := result.Fingerprint.Clone()
		prospective.Fingerprint = &fingerprintCopy
	}
	prospective.FinalizationVerified = true
	prospective.PackageReference = collection.OutputDestination
	prospective.State = deriveRunState(prospective.Records)
	if cancelled && prospective.State == RunPartial {
		prospective.OrchestrationReason = OrchestrationCancelled
	}
	if err := prospective.Validate(); err != nil {
		return FinalizationResult{}, fmt.Errorf("validate prospective first-stage result: %w", errors.Join(err, session.Abort()))
	}
	metadata := receipt.FirstStagePackageMetadata{
		SchemaVersion:       receipt.SchemaVersion,
		ManifestVersion:     receipt.ManifestVersion,
		ProductVersion:      productVersion,
		RuntimeArtifact:     receipt.FirstStageRuntimeArtifact,
		CollectionID:        collection.CollectionID,
		CaseID:              collection.CaseID,
		StartedAt:           collection.StartedAt,
		FinishedAt:          finishedAtText,
		TargetFingerprint:   target.Clone(),
		RunState:            string(prospective.State),
		OrchestrationReason: string(prospective.OrchestrationReason),
		ReceiptReferences:   receiptReferences,
		ArtifactReferences:  make([]receipt.ArtifactReference, 0),
	}
	if artifactRef != nil {
		metadata.ArtifactReferences = []receipt.ArtifactReference{*artifactRef}
	}
	if err := session.Finalize(ctx, metadata, receipts); err != nil {
		return FinalizationResult{}, errors.Join(err, session.Abort())
	}
	return FinalizationResult{Verified: true, PackageReference: collection.OutputDestination, References: references}, nil
}

func isNilFirstStagePackageSession(session firstStagePackageSession) bool {
	if session == nil {
		return true
	}
	value := reflect.ValueOf(session)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func cloneCapability(value *capability.Capability) *capability.Capability {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
