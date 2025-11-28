//go:build windows
// +build windows

package opc

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"github.com/sirupsen/logrus"
)

const (
	ReConnectInterval   = 1 * time.Second
	ReconnectTimes      = 100
	AddTagRetryTimes    = 100
	AddTagRetryInterval = 500 * time.Millisecond
)

func init() {
	OleInit()
}

// OleInit initializes OLE.
func OleInit() {
	ole.CoInitializeEx(0, 0)
}

// OleRelease realeses OLE resources in opcAutomation.
func OleRelease() {
	ole.CoUninitialize()
}

// AutomationObject loads the OPC Automation Wrapper and handles to connection to the OPC Server.
type AutomationObject struct {
	unknown *ole.IUnknown
	object  *ole.IDispatch
	logger  *logrus.Entry
}

type Tree struct {
	Name     string
	Parent   *Tree
	Branches []*Tree
	Leaves   []Leaf
}

// Leaf contains the OPC tag and forms part of the Tree struct for the  OPC browser
type Leaf struct {
	Name string
	Tag  string
}

// CreateBrowser returns the OPCBrowser object from the OPCServer.
// It only works if there is a successful connection.
func (ao *AutomationObject) CreateBrowser() (*Tree, error) {
	// create browser
	browser, err := oleutil.CallMethod(ao.object, "CreateBrowser")
	if err != nil {
		ao.logger.Errorf("failed to create Browser, err: %s", err)
		return nil, fmt.Errorf("failed to create Browser, err: %s", err)
	}

	// move to root
	oleutil.MustCallMethod(browser.ToIDispatch(), "MoveToRoot")

	// create tree
	root := Tree{"root", nil, []*Tree{}, []Leaf{}}
	buildTree(browser.ToIDispatch(), &root, ao.logger)

	return &root, nil
}

// buildTree runs through the OPCBrowser and creates a tree with the OPC tags
func buildTree(browser *ole.IDispatch, branch *Tree, logger *logrus.Entry) {
	var count int32

	logger.Tracef("Entering branch: %s", branch.Name)

	// loop through leafs
	oleutil.MustCallMethod(browser, "ShowLeafs").ToIDispatch()
	count = oleutil.MustGetProperty(browser, "Count").Value().(int32)

	logger.Tracef("Leafs count:%d", count)

	for i := 1; i <= int(count); i++ {

		item := oleutil.MustCallMethod(browser, "Item", i).Value()
		tag := oleutil.MustCallMethod(browser, "GetItemID", item).Value()

		l := Leaf{Name: item.(string), Tag: tag.(string)}
		logger.Tracef("Item index:%d, Leaf name:%s, tag:%s", i, l.Name, l.Tag)

		branch.Leaves = append(branch.Leaves, l)
	}

	// loop through branches
	oleutil.MustCallMethod(browser, "ShowBranches").ToIDispatch()
	count = oleutil.MustGetProperty(browser, "Count").Value().(int32)
	logger.Tracef("Branches count:%d", count)

	for i := 1; i <= int(count); i++ {

		nextName := oleutil.MustCallMethod(browser, "Item", i).Value()

		logger.Tracef("Branch index:%d, next branch name:%s", i, nextName.(string))

		// move down
		oleutil.MustCallMethod(browser, "MoveDown", nextName)

		// recursively populate tree
		nextBranch := Tree{nextName.(string), branch, []*Tree{}, []Leaf{}}
		branch.Branches = append(branch.Branches, &nextBranch)
		buildTree(browser, &nextBranch, logger)

		// move up and set branches again
		oleutil.MustCallMethod(browser, "MoveUp")
		oleutil.MustCallMethod(browser, "ShowBranches").ToIDispatch()
	}

	logger.Tracef("Exiting branch:%s", branch.Name)

}

// Connect establishes a connection to the OPC Server on node.
// It returns a reference to AutomationItems and error message.
func (ao *AutomationObject) Connect(server string, node string) (*AutomationItems, error) {

	// make sure there is not active connection before trying to connect
	ao.disconnect()

	// try to connect to opc server and check for error
	ao.logger.Debugf("Connecting to %s on node %s", server, node)
	_, err := oleutil.CallMethod(ao.object, "Connect", server, node)
	if err != nil {
		ao.logger.Errorf("connection failed. Error: %s", err)
		return nil, fmt.Errorf("connection failed. Error: %s", err)
	}

	// set up opc groups and items
	opcGroups, err := oleutil.GetProperty(ao.object, "OPCGroups")
	if err != nil {
		ao.logger.Errorf("failed to get OPC groups property. Error: %s", err)
		return nil, fmt.Errorf("failed to get OPC groups property. Error: %s", err)
	}
	opcGrp, err := oleutil.CallMethod(opcGroups.ToIDispatch(), "Add")
	if err != nil {
		ao.logger.Errorf("failed to add OPC group. Error: %s", err)
		return nil, fmt.Errorf("failed to add OPC group. Error: %s", err)
	}
	addItemObject, err := oleutil.GetProperty(opcGrp.ToIDispatch(), "OPCItems")
	if err != nil {
		ao.logger.Errorf("cannot get OPC Items. Error: %s", err)
		return nil, fmt.Errorf("cannot get OPC Items. Error: %s", err)
	}

	opcGroups.ToIDispatch().Release()
	opcGrp.ToIDispatch().Release()

	ao.logger.Debug("Connected successfully")

	return NewAutomationItems(addItemObject.ToIDispatch(), ao.logger), nil
}

// TryConnect loops over the nodes array and tries to connect to any of the servers.
func (ao *AutomationObject) TryConnect(server string, nodes []string) (*AutomationItems, error) {
	var errResult string
	for _, node := range nodes {
		items, err := ao.Connect(server, node)
		if err == nil {
			return items, err
		}
		ao.logger.Warnf("TryConnect node %s failed. Error: %s", node, err)
		errResult = errResult + err.Error() + "\n"
	}
	return nil, errors.New("TryConnect was not successful: " + errResult)
}

// IsConnected check if the server is properly connected and up and running.
func (ao *AutomationObject) IsConnected() bool {
	if ao.object == nil {
		return false
	}
	stateVt, err := oleutil.GetProperty(ao.object, "ServerState")
	if err != nil {
		ao.logger.Warnf("GetProperty call for ServerState failed, err:%s", err)
		return false
	}
	status := stateVt.Value().(int32)
	// some OPC server return status OPCNoconfig when no license is available, so we should consider it as connected
	// some OPC server return status OPCTest when in test mode, so we should consider it as connected
	ao.logger.Debugf("OPC Server IsConnected status:%d", status)
	if status != OPCRunning && status != OPCNoconfig && status != OPCTest {
		return false
	}
	return true
}

// Disconnect checks if connected to server and if so, it calls 'disconnect'
func (ao *AutomationObject) disconnect() {
	if ao.IsConnected() {
		ao.logger.Debug("Disconnecting from server")
		_, err := oleutil.CallMethod(ao.object, "Disconnect")
		if err != nil {
			ao.logger.Errorf("Failed to disconnect. Error: %s", err)
		}
	}
}

// Close releases the OLE objects in the AutomationObject.
func (ao *AutomationObject) Close() {
	ao.logger.Debugf("Closing AutomationObject")
	if ao.object != nil {
		ao.disconnect()
		ao.object.Release()
	}
	if ao.unknown != nil {
		ao.unknown.Release()
	}
}

// NewAutomationObject connects to the COM object based on available wrappers.
func NewAutomationObject(logger *logrus.Entry) (*AutomationObject, error) {
	wrapper := "Graybox.OPC.DAWrapper.1"
	var err error
	var unknown *ole.IUnknown
	unknown, err = oleutil.CreateObject(wrapper)
	if err != nil {
		logger.Errorf("Could not load OPC Automation object with wrapper: %s", wrapper)
		return nil, err
	}

	opc, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		logger.Errorf("could not QueryInterface IDispatch: %s", err)
		return nil, fmt.Errorf("could not QueryInterface IDispatch: %s", err)
	}
	object := &AutomationObject{
		unknown: unknown,
		object:  opc,
		logger:  logger,
	}
	return object, nil
}

// AutomationItems store the OPCItems from OPCGroup and does the bookkeeping
// for the individual OPC items. Tags can added, removed, and read.
type AutomationItems struct {
	addItemObject *ole.IDispatch
	items         map[string]*ole.IDispatch
	logger        *logrus.Entry
}

// addSingle adds the tag and returns an error. Client handles are not implemented yet.
func (ai *AutomationItems) addSingle(tag string) error {
	ai.logger.Debugf("Adding item tag: %s", tag)
	clientHandle := int32(1)
	item, err := oleutil.CallMethod(ai.addItemObject, "AddItem", tag, clientHandle)
	if err != nil {
		ai.logger.Errorf("failed to add item tag. tag:%s, Error: %s", tag, err)
		return fmt.Errorf("failed to add item tag. tag:%s, Error: %s", tag, err)
	}
	ai.logger.Debugf("Added item tag: %s", tag)
	ai.items[tag] = item.ToIDispatch()
	return nil
}

// Add accepts a variadic parameters of tags.
func (ai *AutomationItems) Add(tags ...string) error {
	var errorList []error
	for _, tag := range tags {
		err := ai.addSingle(tag)
		if err != nil {
			errorList = append(errorList, fmt.Errorf("failed to add item tag: %s. Error: %s", tag, err))
		}
	}
	if len(errorList) == 0 {
		return nil
	}
	return errors.Join(errorList...)
}

// Remove removes the tag.
func (ai *AutomationItems) Remove(tag string) {
	ai.logger.Debugf("removing item tag: %s", tag)
	item, ok := ai.items[tag]
	if ok {
		ai.logger.Debugf("release item tag: %s", tag)
		item.Release()
	}
	delete(ai.items, tag)
}

/*
 * FIX:
 * some opc servers sometimes returns an int32 Quality, that produces panic
 */
func ensureInt16(q interface{}) int16 {
	if v16, ok := q.(int16); ok {
		return v16
	}
	if v32, ok := q.(int32); ok && v32 >= -32768 && v32 < 32768 {
		return int16(v32)
	}
	return 0
}

// readFromOPC reads from the server and returns an Item and error.
func (ai *AutomationItems) readFromOpc(opcitem *ole.IDispatch) (Item, error) {
	v := ole.NewVariant(ole.VT_R4, 0)
	defer v.Clear()
	q := ole.NewVariant(ole.VT_INT, 0)
	ts := ole.NewVariant(ole.VT_DATE, 0)

	_, err := oleutil.CallMethod(opcitem, "Read", OPCCache, &v, &q, &ts)

	if err != nil {
		return Item{}, err
	}

	return Item{
		Value:     v.Value(),
		Quality:   ensureInt16(q.Value()),
		Timestamp: ts.Value().(time.Time),
	}, nil
}

// Close closes the OLE objects in AutomationItems.
func (ai *AutomationItems) Close() {
	if ai != nil {
		for key, opcitem := range ai.items {
			ai.logger.Debugf("releasing item tag: %s", key)
			opcitem.Release()
			delete(ai.items, key)
		}
		ai.logger.Debugf("releasing addItemObject")
		ai.addItemObject.Release()
	}
}

// NewAutomationItems returns a new AutomationItems instance.
func NewAutomationItems(opcitems *ole.IDispatch, logger *logrus.Entry) *AutomationItems {
	return &AutomationItems{
		addItemObject: opcitems,
		items:         make(map[string]*ole.IDispatch),
		logger:        logger,
	}
}

// opcRealServer implements the Connection interface.
// It has the AutomationObject embedded for connecting to the server
// and an AutomationItems to facilitate the OPC items bookkeeping.
type opcConnectionImpl struct {
	*AutomationObject
	*AutomationItems
	Server string
	Nodes  []string
	mu     sync.RWMutex
	logger *logrus.Entry
}

// Read returns a map of the values of all added tags.
func (conn *opcConnectionImpl) Read() map[string]Item {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	allTags := make(map[string]Item)
	for tag, opcitem := range conn.AutomationItems.items {
		item, err := conn.AutomationItems.readFromOpc(opcitem)
		if err != nil {
			conn.logger.Warnf("Cannot read %s: %s. Trying to fix.", tag, err)
			conn.fix()
			continue
		}
		allTags[tag] = item
	}
	return allTags
}

// Tags returns the currently active tags
func (conn *opcConnectionImpl) Tags() []string {
	var tags []string
	if conn.AutomationItems != nil {
		for tag, _ := range conn.AutomationItems.items {
			tags = append(tags, tag)
		}
	}
	return tags
}

// fix tries to reconnect if connection is lost by creating a new connection
// with AutomationObject and creating a new AutomationItems instance.
func (conn *opcConnectionImpl) fix() {
	var err error
	if !conn.IsConnected() {
		conn.logger.Warnf("Connection not established. Trying to reconnect.")
		tags := conn.Tags()
		reconnected := false
		reconnectTimes := 0
		for i := 0; i < ReconnectTimes; i++ {
			reconnectTimes++
			conn.logger.Warnf("Reconnection attempt %d/%d", reconnectTimes, ReconnectTimes)
			conn.AutomationItems.Close()
			conn.AutomationItems, err = conn.TryConnect(conn.Server, conn.Nodes)
			if err != nil {
				conn.logger.Warnf("try to reconnect failed: %s, will retry in 1 second", err)
				time.Sleep(ReConnectInterval)
				continue
			}
			conn.logger.Info("Successfully reconnected to server, adding tags back.")
			conn.reAddTags(tags)
			conn.logger.Info("readd tags back successful after reconnection.")
			reconnected = true
			break
		}
		if !reconnected {
			conn.logger.Panic("Could not reconnect to server, aborting fix.")
		}
	}
}

// reAddTags tries to re-add the tags after reconnection
func (conn *opcConnectionImpl) reAddTags(tags []string) {
	for i, tag := range tags {
		conn.logger.Debugf("Re-adding tag %d/%d: %s", i+1, len(tags), tag)
		reAddSuccess := false
		for retryTimes := 0; retryTimes < AddTagRetryTimes; retryTimes++ {
			err := conn.addSingle(tag)
			if err != nil {
				if retryTimes == AddTagRetryTimes-1 {
					conn.logger.Errorf("Failed to re-add tag %s after %d retries: %s, giving up", tag, retryTimes, err)
					break
				}
				conn.logger.Warnf("Failed to re-add tag %s: %s, retry times:%d, retrying after 500 millseconds", tag, err, retryTimes)
				time.Sleep(AddTagRetryInterval)
			} else {
				reAddSuccess = true
				break
			}
		}
		if !reAddSuccess {
			conn.logger.Panic("Could not re-add all tags, aborting fix.")
		}
	}
}

// Close closes the embedded types.
func (conn *opcConnectionImpl) Close() {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.AutomationObject != nil {
		conn.AutomationObject.Close()
	}
	if conn.AutomationItems != nil {
		conn.AutomationItems.Close()
	}
}

// NewConnection establishes a connection to the OpcServer object.
func NewConnection(server string, nodes []string, tags []string, logger *logrus.Entry) (Connection, error) {
	object, err := NewAutomationObject(logger)
	if err != nil {
		return nil, err
	}
	items, err := object.TryConnect(server, nodes)
	if err != nil {
		object.Close()
		return nil, err
	}
	err = items.Add(tags...)
	if err != nil {
		items.Close()
		object.Close()
		return nil, err
	}
	conn := opcConnectionImpl{
		AutomationObject: object,
		AutomationItems:  items,
		Server:           server,
		Nodes:            nodes,
		logger:           logger,
	}

	return &conn, nil
}

// CreateBrowser creates an opc browser representation
func CreateBrowser(server string, nodes []string, logger *logrus.Entry) (*Tree, error) {
	object, err := NewAutomationObject(logger)
	if err != nil {
		logger.Errorf("Could not create automation object: %s", err)
		return nil, err
	}
	defer object.Close()
	_, err = object.TryConnect(server, nodes)
	if err != nil {
		logger.Errorf("Cannot connect to %s: %s", server, err)
		return nil, err
	}
	return object.CreateBrowser()
}
