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
	DefaultReConnectInterval           = 1 * time.Second
	DefaultReconnectTimes              = 100
	DefaultAddTagRetryTimes            = 100
	DefaultAddTagRetryInterval         = 500 * time.Millisecond
	DefaultFailedReadsToForceReconnect = 50
)

func init() {
	OleInit()
}

// OleInit initializes OLE.
func OleInit() {
	_ = ole.CoInitializeEx(0, 0)
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
		err = TryGetOPCError(err)
		ao.logger.Errorf("failed to create Browser, err: %s", err)
		return nil, fmt.Errorf("failed to create Browser, err: %s", err)
	}
	browserI := browser.ToIDispatch()
	defer func() {
		browserI.Release()
	}()
	// move to root
	oleutil.MustCallMethod(browserI, "MoveToRoot")

	// create tree
	root := Tree{"root", nil, []*Tree{}, []Leaf{}}
	buildTree(browserI, &root, ao.logger)

	return &root, nil
}

// buildTree runs through the OPCBrowser and creates a tree with the OPC tags
func buildTree(browser *ole.IDispatch, branch *Tree, logger *logrus.Entry) {
	var count int32

	logger.Tracef("Entering branch: %s", branch.Name)

	// loop through leafs
	oleutil.MustCallMethod(browser, "ShowLeafs")
	count = oleutil.MustGetProperty(browser, "Count").Value().(int32)

	logger.Tracef("Leafs count:%d", count)

	for i := 1; i <= int(count); i++ {
		item := callMethodGetString(logger, browser, "Item", i)
		tag := callMethodGetString(logger, browser, "GetItemID", item)
		l := Leaf{Name: item, Tag: tag}
		logger.Tracef("Item index:%d, Leaf name:%s, tag:%s", i, l.Name, l.Tag)

		branch.Leaves = append(branch.Leaves, l)
	}

	// loop through branches
	oleutil.MustCallMethod(browser, "ShowBranches")
	count = oleutil.MustGetProperty(browser, "Count").Value().(int32)
	logger.Tracef("Branches count:%d", count)

	for i := 1; i <= int(count); i++ {
		nextName := callMethodGetString(logger, browser, "Item", i)
		logger.Tracef("Branch index:%d, next branch name:%s", i, nextName)

		// move down
		oleutil.MustCallMethod(browser, "MoveDown", nextName)

		// recursively populate tree
		nextBranch := Tree{nextName, branch, []*Tree{}, []Leaf{}}
		branch.Branches = append(branch.Branches, &nextBranch)
		buildTree(browser, &nextBranch, logger)

		// move up and set branches again
		oleutil.MustCallMethod(browser, "MoveUp")
		oleutil.MustCallMethod(browser, "ShowBranches")
	}

	logger.Tracef("Exiting branch:%s", branch.Name)
}

func callMethodGetString(logger *logrus.Entry, dispatch *ole.IDispatch, methodName string, params ...interface{}) string {
	logger.Debugf("Calling method: %s, params: %v", methodName, params)
	result, err := oleutil.CallMethod(dispatch, methodName, params...)
	if err != nil {
		err = TryGetOPCError(err)
		logger.Panicf("failed to call method: %s, err: %s", methodName, err)
	}
	strVal := result.Value().(string)
	err = result.Clear()
	if err != nil {
		// ignore error on clear
		logger.Errorf("failed to clear variant from method %s: %s", methodName, err)
	}
	return strVal
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
		err = TryGetOPCError(err)
		ao.logger.Errorf("connection failed. Error: %s", err)
		return nil, fmt.Errorf("connection failed. Error: %s", err)
	}

	// set up opc groups and items
	opcGroups, err := oleutil.GetProperty(ao.object, "OPCGroups")
	if err != nil {
		err = TryGetOPCError(err)
		ao.logger.Errorf("failed to get OPC groups property. Error: %s", err)
		return nil, fmt.Errorf("failed to get OPC groups property. Error: %s", err)
	}
	opcGroupsI := opcGroups.ToIDispatch()
	defer func() {
		opcGroupsI.Release()
	}()

	opcGroup, err := oleutil.CallMethod(opcGroupsI, "Add")
	if err != nil {
		err = TryGetOPCError(err)
		ao.logger.Errorf("failed to add OPC group. Error: %s", err)
		return nil, fmt.Errorf("failed to add OPC group. Error: %s", err)
	}
	opcGroupI := opcGroup.ToIDispatch()
	defer func() {
		opcGroupI.Release()
	}()

	itemObject, err := oleutil.GetProperty(opcGroupI, "OPCItems")
	if err != nil {
		err = TryGetOPCError(err)
		ao.logger.Errorf("cannot get OPC Items. Error: %s", err)
		return nil, fmt.Errorf("cannot get OPC Items. Error: %s", err)
	}

	ao.logger.Debug("Connected successfully")

	return NewAutomationItems(itemObject.ToIDispatch(), ao.logger), nil
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
		err = TryGetOPCError(err)
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
		_, err := oleutil.CallMethod(ao.object, "Disconnect")
		if err != nil {
			err = TryGetOPCError(err)
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
		unknown.Release()
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
	itemI     *ole.IDispatch
	items     map[string]*ole.IDispatch
	cacheTags []string
	logger    *logrus.Entry
}

// addSingle adds the tag and returns an error. Client handles are not implemented yet.
func (ai *AutomationItems) addSingle(tag string) error {
	ai.logger.Debugf("Adding item tag: %s", tag)
	clientHandle := int32(1)
	item, err := oleutil.CallMethod(ai.itemI, "AddItem", tag, clientHandle)
	if err != nil {
		err = TryGetOPCError(err)
		ai.logger.Errorf("failed to add item tag. tag:%s, Error: %s", tag, err)
		return fmt.Errorf("failed to add item tag. tag:%s, Error: %s", tag, err)
	}
	ai.logger.Debugf("Added item tag: %s", tag)
	ai.items[tag] = item.ToIDispatch()
	return nil
}

// Add accepts a variadic parameters of tags.
func (ai *AutomationItems) Add(tags ...string) error {
	defer ai.updateCacheTags()
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
	defer ai.updateCacheTags()
	ai.logger.Debugf("removing item tag: %s", tag)
	item, ok := ai.items[tag]
	if ok {
		ai.logger.Debugf("release item tag: %s", tag)
		item.Release()
	}
	delete(ai.items, tag)
}

func (ai *AutomationItems) Tags() []string {
	var tags []string
	if ai != nil {
		for tag := range ai.items {
			tags = append(tags, tag)
		}
	}
	return tags
}

func (ai *AutomationItems) updateCacheTags() {
	cacheTags := make([]string, 0, len(ai.cacheTags))
	for key := range ai.items {
		cacheTags = append(cacheTags, key)
	}
	ai.cacheTags = cacheTags
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
func (ai *AutomationItems) readFromOpc(tag string, opcitem *ole.IDispatch) (Item, error) {
	v := ole.NewVariant(ole.VT_R4, 0)
	defer func() {
		clearErr := v.Clear()
		if clearErr != nil {
			ai.logger.Errorf("failed to clear variant: %s,tag: %s", clearErr, tag)
		}
	}()
	q := ole.NewVariant(ole.VT_INT, 0)
	ts := ole.NewVariant(ole.VT_DATE, 0)

	_, err := oleutil.CallMethod(opcitem, "Read", OPCCache, &v, &q, &ts)
	if err != nil {
		err = TryGetOPCError(err)
		ai.logger.Errorf("failed to read from opc item. Error: %s,tag: %s", err, tag)
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
		ai.logger.Debugf("releasing itemI")
		ai.itemI.Release()
	}
}

// NewAutomationItems returns a new AutomationItems instance.
func NewAutomationItems(itemI *ole.IDispatch, logger *logrus.Entry) *AutomationItems {
	return &AutomationItems{
		itemI:  itemI,
		items:  make(map[string]*ole.IDispatch),
		logger: logger,
	}
}

// OpcConnectionImpl implements the Connection interface.
// It has the AutomationObject embedded for connecting to the server
// and an AutomationItems to facilitate the OPC items bookkeeping.
// Exported for testing purpose.
type OpcConnectionImpl struct {
	Object                      *AutomationObject
	Items                       *AutomationItems
	Server                      string
	Nodes                       []string
	mu                          sync.RWMutex
	logger                      *logrus.Entry
	reconnectTimes              int
	addTagRetryTimes            int
	reconnectInterval           time.Duration
	addTagRetryInterval         time.Duration
	failedReadsToForceReconnect int

	readFailedTimes int
}

func (conn *OpcConnectionImpl) Add(s ...string) error {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return conn.Items.Add(s...)
}

func (conn *OpcConnectionImpl) Remove(s string) {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	conn.Items.Remove(s)
}

// Read returns a map of the values of all added tags.
func (conn *OpcConnectionImpl) Read() map[string]Item {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	allTags := make(map[string]Item)
	tags := conn.Items.cacheTags
	for i := 0; i < len(tags); i++ {
		tag := tags[i]
		opcItem := conn.Items.items[tag]
		item, err := conn.Items.readFromOpc(tag, opcItem)
		if err != nil {
			conn.readFailedTimes += 1
			conn.logger.Errorf("Cannot read %s: %s. Total Failed count: %d, Trying to fix.", tag, err, conn.readFailedTimes)
			if conn.readFailedTimes >= conn.failedReadsToForceReconnect {
				conn.logger.Warnf("Read failed %d times, force reconnect.", conn.readFailedTimes)
				conn.Fix(true)
			} else {
				conn.Fix(false)
			}
			continue
		}
		allTags[tag] = item
	}
	return allTags
}

// Tags returns the currently active tags
func (conn *OpcConnectionImpl) Tags() []string {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return conn.Items.Tags()
}

// Fix tries to reconnect if connection is lost by creating a new connection
// with AutomationObject and creating a new AutomationItems instance.
// Exported for testing purpose.
func (conn *OpcConnectionImpl) Fix(force bool) {
	var err error
	if force || !conn.Object.IsConnected() {
		conn.logger.Warnf("[RECONNECT] Trying to reconnect.")
		tags := conn.Items.Tags()
		reconnected := false
		reconnectTimes := 0
		reconnectStartTime := time.Now()
		for i := 0; i < conn.reconnectTimes; i++ {
			reconnectTimes++
			conn.logger.Warnf("Reconnection attempt %d/%d", reconnectTimes, conn.reconnectTimes)
			conn.Items.Close()
			conn.Items, err = conn.Object.TryConnect(conn.Server, conn.Nodes)
			if err != nil {
				conn.logger.Warnf("try to reconnect failed: %s, will retry in %d millseconds", err, conn.reconnectInterval.Milliseconds())
				time.Sleep(conn.reconnectInterval)
				continue
			}
			conn.logger.Info("Successfully reconnected to server, adding tags back.")
			reAddTagStartTime := time.Now()
			addTagsSuccess := conn.reAddTags(tags)
			if addTagsSuccess {
				// re-adding tags successful, break
				conn.logger.Infof("readd tags back successful after reconnection, cost: %d us.", time.Since(reAddTagStartTime).Microseconds())
				conn.Items.updateCacheTags()
				reconnected = true
				break
			} else {
				// re-adding tags failed, panic
				conn.logger.Errorf("[RECONNECT] Reconnection failed after %d attempts, due to re-adding tags failed, total cost time: %d us.", reconnectTimes, time.Since(reconnectStartTime).Microseconds())
				conn.logger.Panic("re-adding tags failed after reconnection, aborting fix.")
			}
		}
		if !reconnected {
			// reconnect failed after max retries
			conn.logger.Errorf("[RECONNECT] Reconnection failed after %d attempts, total cost time: %d us.", reconnectTimes, time.Since(reconnectStartTime).Microseconds())
			conn.logger.Panic("Could not reconnect to server, aborting fix.")
		}
		conn.logger.Infof("[RECONNECT] Reconnection successful after %d attempts, total cost time: %d us.", reconnectTimes, time.Since(reconnectStartTime).Microseconds())
		conn.logger.Debug("cleaned up readFailedTimes after successful reconnection.")
		conn.readFailedTimes = 0
	} else {
		conn.logger.Warnf("Connection is established. No fix action taken.")
	}
}

// reAddTags tries to re-add the tags after reconnection
func (conn *OpcConnectionImpl) reAddTags(tags []string) bool {
	for i, tag := range tags {
		conn.logger.Debugf("Re-adding tag %d/%d: %s", i+1, len(tags), tag)
		reAddSuccess := false
		for retryTimes := 1; retryTimes <= conn.addTagRetryTimes; retryTimes++ {
			err := conn.Items.addSingle(tag)
			if err != nil {
				if retryTimes == conn.addTagRetryTimes {
					conn.logger.Errorf("Failed to re-add tag %s after %d retries: %s, giving up", tag, retryTimes, err)
					break
				}
				conn.logger.Warnf("Failed to re-add tag %s: %s, retry times:%d, retrying after %d millseconds", tag, err, retryTimes, conn.addTagRetryInterval.Milliseconds())
				time.Sleep(conn.addTagRetryInterval)
			} else {
				reAddSuccess = true
				break
			}
		}
		if !reAddSuccess {
			conn.logger.Errorf("[RECONNECT] Could not re-add tag: %s", tag)
			return false
		}
	}
	return true
}

// Close closes the embedded types.
func (conn *OpcConnectionImpl) Close() {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.Items != nil {
		conn.Items.Close()
	}
	if conn.Object != nil {
		conn.Object.Close()
	}
}

type ConnectionConfig struct {
	ReconnectTimes              int
	ReconnectInterval           time.Duration
	AddTagRetryTimes            int
	AddTagRetryInterval         time.Duration
	FailedReadsToForceReconnect int
}

func DefaultConnectionConfig() *ConnectionConfig {
	return &ConnectionConfig{
		ReconnectTimes:              DefaultReconnectTimes,
		ReconnectInterval:           DefaultReConnectInterval,
		AddTagRetryTimes:            DefaultAddTagRetryTimes,
		AddTagRetryInterval:         DefaultAddTagRetryInterval,
		FailedReadsToForceReconnect: DefaultFailedReadsToForceReconnect,
	}
}

// NewConnection establishes a connection to the OpcServer object.
func NewConnection(server string, nodes []string, tags []string, config *ConnectionConfig, logger *logrus.Entry) (Connection, error) {
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
	conn := OpcConnectionImpl{
		Object:                      object,
		Items:                       items,
		Server:                      server,
		Nodes:                       nodes,
		logger:                      logger,
		reconnectTimes:              config.ReconnectTimes,
		reconnectInterval:           config.ReconnectInterval,
		addTagRetryTimes:            config.AddTagRetryTimes,
		addTagRetryInterval:         config.AddTagRetryInterval,
		failedReadsToForceReconnect: config.FailedReadsToForceReconnect,
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
	items, err := object.TryConnect(server, nodes)
	if err != nil {
		logger.Errorf("Cannot connect to %s: %s", server, err)
		return nil, err
	}
	defer items.Close()
	return object.CreateBrowser()
}
