package onet

import (
	"github.com/cypherium/cypherBFT/onet/network"
)

// Context represents the methods that are available to a service.
type Context struct {
	overlay    *Overlay
	server     *Server
	serviceID  ServiceID
	manager    *serviceManager
	bucketName []byte
}

// defaultContext is the implementation of the Context interface. It is
// instantiated for each Service.
func newContext(c *Server, o *Overlay, servID ServiceID, manager *serviceManager) *Context {
	return &Context{
		overlay:    o,
		server:     c,
		serviceID:  servID,
		manager:    manager,
		bucketName: []byte(ServiceFactory.Name(servID)),
	}
}

// NewTreeNodeInstance creates a TreeNodeInstance that is bound to a
// service instead of the Overlay.
func (c *Context) NewTreeNodeInstance(t *Tree, tn *TreeNode, protoName string) *TreeNodeInstance {
	io := c.overlay.protoIO.getByName(protoName)
	return c.overlay.NewTreeNodeInstanceFromService(t, tn, ProtocolNameToID(protoName), c.serviceID, io)
}

func (c *Context) SendRaw1(ps ...interface{}) {
	si := ps[0].(*network.ServerIdentity)
	msg := ps[1]
	bForeConnect := ps[2].(bool)
	c.server.Send(si, msg, bForeConnect)
}

// SendRaw sends a message to the ServerIdentity.
func (c *Context) SendRaw(si *network.ServerIdentity, msg interface{}, bForeConnect bool) error {
	_, err := c.server.Send(si, msg, bForeConnect)
	return err
}

// ServerIdentity returns this server's identity.
func (c *Context) ServerIdentity() *network.ServerIdentity {
	return c.server.ServerIdentity
}

// Suite returns the suite for the context's associated server.
func (c *Context) Suite() network.Suite {
	return c.server.Suite()
}

// ServiceID returns the service-id.
func (c *Context) ServiceID() ServiceID {
	return c.serviceID
}

// CreateProtocol returns a ProtocolInstance bound to the service.
func (c *Context) CreateProtocol(name string, t *Tree) (ProtocolInstance, error) {
	pi, err := c.overlay.CreateProtocol(name, t, c.serviceID)
	return pi, err
}

// ProtocolRegister signs up a new protocol to this Server. Contrary go
// GlobalProtocolRegister, the protocol registered here is tied to that server.
// This is useful for simulations where more than one Server exists in the
// global namespace.
// It returns the ID of the protocol.
func (c *Context) ProtocolRegister(name string, protocol NewProtocol) (ProtocolID, error) {
	return c.server.ProtocolRegister(name, protocol)
}

// RegisterProtocolInstance registers a new instance of a protocol using overlay.
func (c *Context) RegisterProtocolInstance(pi ProtocolInstance) error {
	return c.overlay.RegisterProtocolInstance(pi)
}

// ReportStatus returns all status of the services.
func (c *Context) ReportStatus() map[string]*Status {
	return c.server.statusReporterStruct.ReportStatus()
}

// RegisterStatusReporter registers a new StatusReporter.
func (c *Context) RegisterStatusReporter(name string, s StatusReporter) {
	c.server.statusReporterStruct.RegisterStatusReporter(name, s)
}

// RegisterProcessor overrides the RegisterProcessor methods of the Dispatcher.
// It delegates the dispatching to the serviceManager.
func (c *Context) RegisterProcessor(p network.Processor, msgType network.MessageTypeID) {
	c.manager.registerProcessor(p, msgType)
}

// RegisterProcessorFunc takes a message-type and a function that will be called
// if this message-type is received.
func (c *Context) RegisterProcessorFunc(msgType network.MessageTypeID, fn func(*network.Envelope)) {
	c.manager.registerProcessorFunc(msgType, fn)
}

// RegisterMessageProxy registers a message proxy only for this server /
// overlay
func (c *Context) RegisterMessageProxy(m MessageProxy) {
	c.overlay.RegisterMessageProxy(m)
}

// Service returns the corresponding service.
func (c *Context) Service(name string) Service {
	return c.manager.service(name)
}

// String returns the host it's running on.
func (c *Context) String() string {
	return c.server.ServerIdentity.String()
}
