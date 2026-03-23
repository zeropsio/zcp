package platform

import (
	"time"

	"github.com/zeropsio/zerops-go/dto/output"
)

func mapEsProcessEvent(p output.EsProcess) ProcessEvent {
	status := p.Status.String()
	switch status {
	case "DONE":
		status = "FINISHED"
	case statusCancelled:
		status = "CANCELED"
	}

	serviceStacks := make([]ServiceStackRef, 0, len(p.ServiceStacks))
	for _, ss := range p.ServiceStacks {
		serviceStacks = append(serviceStacks, ServiceStackRef{
			ID:   ss.Id.TypedString().String(),
			Name: ss.Name.String(),
		})
	}

	var started *string
	if s, ok := p.Started.Get(); ok {
		v := s.Format(time.RFC3339Nano)
		started = &v
	}
	var finished *string
	if f, ok := p.Finished.Get(); ok {
		v := f.Format(time.RFC3339Nano)
		finished = &v
	}

	// Extract FailReason from PublicMeta if present.
	var failReason *string
	if m, ok := p.PublicMeta.Get(); ok {
		raw := map[string]any(m)
		if fr, ok := raw["failReason"]; ok {
			if s, ok := fr.(string); ok && s != "" {
				failReason = &s
			}
		}
	}

	var user *UserRef
	if fn, ok := p.CreatedByUser.FullName.Get(); ok {
		u := UserRef{FullName: fn.String()}
		if email, ok := p.CreatedByUser.Email.Get(); ok {
			u.Email = email.Native()
		}
		user = &u
	}

	return ProcessEvent{
		ID:              p.Id.TypedString().String(),
		ProjectID:       p.ProjectId.TypedString().String(),
		ServiceStacks:   serviceStacks,
		ActionName:      p.ActionName.String(),
		Status:          status,
		Created:         p.Created.Format(time.RFC3339Nano),
		Started:         started,
		Finished:        finished,
		FailReason:      failReason,
		CreatedByUser:   user,
		CreatedBySystem: p.CreatedBySystem.Native(),
	}
}

func mapEsAppVersionEvent(av output.EsAppVersion) AppVersionEvent {
	event := AppVersionEvent{
		ID:             av.Id.TypedString().String(),
		ProjectID:      av.ProjectId.TypedString().String(),
		ServiceStackID: av.ServiceStackId.TypedString().String(),
		Source:         av.Source.String(),
		Status:         av.Status.String(),
		Sequence:       av.Sequence.Native(),
		Created:        av.Created.Format(time.RFC3339Nano),
		LastUpdate:     av.LastUpdate.Format(time.RFC3339Nano),
	}

	if av.Build != nil {
		bi := &BuildInfo{}
		hasBuild := false
		if ssid, ok := av.Build.ServiceStackId.Get(); ok {
			v := ssid.TypedString().String()
			bi.ServiceStackID = &v
			hasBuild = true
		}
		if ps, ok := av.Build.PipelineStart.Get(); ok {
			v := ps.Format(time.RFC3339Nano)
			bi.PipelineStart = &v
			hasBuild = true
		}
		if pf, ok := av.Build.PipelineFinish.Get(); ok {
			v := pf.Format(time.RFC3339Nano)
			bi.PipelineFinish = &v
			hasBuild = true
		}
		if pf, ok := av.Build.PipelineFailed.Get(); ok {
			v := pf.Format(time.RFC3339Nano)
			bi.PipelineFailed = &v
			hasBuild = true
		}
		if hasBuild {
			event.Build = bi
		}
	}

	return event
}
