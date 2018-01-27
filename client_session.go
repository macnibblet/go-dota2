package dota2

import (
	"context"

	"github.com/Philipp15b/go-steam/protocol/gamecoordinator"
	devents "github.com/paralin/go-dota2/events"
	// gcmm "github.com/paralin/go-dota2/protocol/dota_gcmessages_common_match_management"
	gcsdkm "github.com/paralin/go-dota2/protocol/gcsdk_gcmessages"
	gcsm "github.com/paralin/go-dota2/protocol/gcsystemmsgs"
	"github.com/paralin/go-dota2/state"
)

// SetPlaying informs Steam we are playing / not playing Dota 2.
func (d *Dota2) SetPlaying(playing bool) {
	if playing {
		d.client.GC.SetGamesPlayed(AppID)
	} else {
		d.client.GC.SetGamesPlayed()
		_ = d.accessState(func(ns *state.Dota2State) (changed bool, err error) {
			ns.ClearState()
			return true, nil
		})
	}
}

// SayHello says hello to the Dota2 server, in an attempt to get a session.
func (d *Dota2) SayHello() {
	partnerAccType := gcsdkm.PartnerAccountType_PARTNER_NONE
	engine := gcsdkm.ESourceEngine_k_ESE_Source2
	var clientSessionNeed uint32 = 104
	d.write(uint32(gcsm.EGCBaseClientMsg_k_EMsgGCClientHello), &gcsdkm.CMsgClientHello{
		ClientLauncher:    &partnerAccType,
		Engine:            &engine,
		ClientSessionNeed: &clientSessionNeed,
	})
}

// validateConnectionContext checks if the client is ready or not.
func (d *Dota2) validateConnectionContext() (context.Context, error) {
	d.connectionCtxMtx.Lock()
	defer d.connectionCtxMtx.Unlock()

	cctx := d.connectionCtx
	if cctx == nil {
		return nil, NotReadyErr
	}

	select {
	case <-cctx.Done():
		return nil, NotReadyErr
	default:
		return cctx, nil
	}
}

// handleClientWelcome handles an incoming client welcome event.
func (d *Dota2) handleClientWelcome(packet *gamecoordinator.GCPacket) error {
	welcome := &gcsdkm.CMsgClientWelcome{}
	if err := d.unmarshalBody(packet, welcome); err != nil {
		return err
	}

	d.setConnectionStatus(gcsdkm.GCConnectionStatus_GCConnectionStatus_HAVE_SESSION, nil)
	d.emit(&devents.ClientWelcomed{Welcome: welcome})
	return nil
}

// handleConnectionStatus handles the connection status update event.
func (d *Dota2) handleConnectionStatus(packet *gamecoordinator.GCPacket) error {
	stat := &gcsdkm.CMsgConnectionStatus{}
	if err := d.unmarshalBody(packet, stat); err != nil {
		return err
	}

	if stat.Status == nil {
		return nil
	}

	d.setConnectionStatus(*stat.Status, stat)
	return nil
}

// setConnectionStatus sets the connection status, and emits an event.
// NOTE: do not call from inside accessState.
func (d *Dota2) setConnectionStatus(
	connStatus gcsdkm.GCConnectionStatus,
	update *gcsdkm.CMsgConnectionStatus,
) {
	_ = d.accessState(func(ns *state.Dota2State) (changed bool, err error) {
		if ns.ConnectionStatus == connStatus {
			return false, nil
		}

		oldState := ns.ConnectionStatus
		d.le.WithField("old", oldState.String()).
			WithField("new", connStatus.String()).
			Debug("connection status changed")
		d.emit(&devents.GCConnectionStatusChanged{
			OldState: oldState,
			NewState: connStatus,
			Update:   update,
		})

		ns.ClearState() // every time the state changes, we lose the lobbies / etc
		ns.ConnectionStatus = connStatus
		d.connectionCtxMtx.Lock()
		if d.connectionCtxCancel != nil {
			d.connectionCtxCancel()
			d.connectionCtxCancel = nil
			d.connectionCtx = nil
		}
		if connStatus == gcsdkm.GCConnectionStatus_GCConnectionStatus_HAVE_SESSION {
			d.connectionCtx, d.connectionCtxCancel = context.WithCancel(context.Background())
		}
		d.connectionCtxMtx.Unlock()
		return true, nil
	})
}