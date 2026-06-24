package renderplan

import "fmt"

const (
	FormatShort9x16      = "short-9x16"
	FormatLandscape16x9  = "landscape-16x9"
	KillEffectClean      = "clean"
	KillEffectPunchIn    = "punch-in"
	KillEffectVelocity   = "velocity"
	KillEffectFreezeFlash = "freeze-flash"
	TransitionCut        = "cut"
	TransitionFlash      = "flash"
	TransitionWhip       = "whip"
	TransitionDip        = "dip"
)

// EditRequest is the user-selected editing contract captured from the UI for
// one render. Workers snapshot it into the edit document so a render is
// reproducible without knowing which screen launched it.
type EditRequest struct {
	Format     string `json:"format"`
	KillEffect string `json:"killEffect"`
	Transition string `json:"transition"`
	Intro      bool   `json:"intro"`
	Outro      bool   `json:"outro"`
}

func DefaultEditRequest() EditRequest {
	return EditRequest{
		Format:     FormatShort9x16,
		KillEffect: KillEffectPunchIn,
		Transition: TransitionFlash,
	}
}

func NormalizeEditRequest(req EditRequest) EditRequest {
	def := DefaultEditRequest()
	if req.Format == "" {
		req.Format = def.Format
	}
	if req.KillEffect == "" {
		req.KillEffect = def.KillEffect
	}
	if req.Transition == "" {
		req.Transition = def.Transition
	}
	return req
}

func (r EditRequest) Validate() error {
	switch r.Format {
	case FormatShort9x16, FormatLandscape16x9:
	default:
		return fmt.Errorf("unknown render format %q", r.Format)
	}
	switch r.KillEffect {
	case KillEffectClean, KillEffectPunchIn, KillEffectVelocity, KillEffectFreezeFlash:
	default:
		return fmt.Errorf("unknown kill effect %q", r.KillEffect)
	}
	switch r.Transition {
	case TransitionCut, TransitionFlash, TransitionWhip, TransitionDip:
	default:
		return fmt.Errorf("unknown transition %q", r.Transition)
	}
	return nil
}
