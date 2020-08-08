// Copyright 2020 Rogchap. All Rights Reserved.

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"

	"github.com/golang/protobuf/protoc-gen-go/descriptor"
	"github.com/jhump/protoreflect/dynamic"
	"github.com/therecipe/qt/core"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"

	"rogchap.com/wombat/internal/db"
	"rogchap.com/wombat/internal/model"
)

//go:generate qtmoc
type workspaceController struct {
	core.QObject

	grpcConn      *grpc.ClientConn
	cancelCtxFunc context.CancelFunc
	store         *db.Store

	_ func() `constructor:"init"`

	_ *inputController  `property:"inputCtrl"`
	_ *outputController `property:"outputCtrl"`

	_ *model.WorkspaceOptions `property:"options"`
	_ string                  `property:"connState"`

	_ func(path string)                  `slot:"findProtoFiles"`
	_ func(path string)                  `slot:"addImport"`
	_ func() error                       `slot:"processProtos"`
	_ func(addr string) error            `slot:"connect"`
	_ func(service, method string) error `slot:"send"`
}

func (c *workspaceController) init() {
	c.SetInputCtrl(NewInputController(nil))
	c.SetOutputCtrl(NewOutputController(nil))

	c.SetOptions(model.NewWorkspaceOptions(nil))

	c.ConnectFindProtoFiles(c.findProtoFiles)
	c.ConnectAddImport(c.addImport)
	c.ConnectProcessProtos(c.processProtos)
	c.ConnectConnect(c.connect)
	c.ConnectSend(c.send)

	dbPath := core.QStandardPaths_WritableLocation(core.QStandardPaths__AppDataLocation)
	if isDebug {
		dbPath = filepath.Join(".", ".data")
	}
	c.store = db.NewStore(dbPath)

	w := c.store.Get()
	if w == nil {
		return
	}

	opts := c.Options()
	opts.SetReflect(w.Reflect)
	opts.SetInsecure(w.Insecure)
	opts.SetPlaintext(w.Plaintext)
	opts.SetRootca(w.Rootca)
	opts.SetClientcert(w.Clientcert)
	opts.SetClientkey(w.Clientkey)
	opts.ProtoListModel().SetStringList(w.ProtoFiles)
	opts.ImportListModel().SetStringList(w.ImportFiles)
	c.processProtos()
	c.connect(w.Addr)
}

func (c *workspaceController) findProtoFiles(path string) {
	path = core.NewQUrl3(path, core.QUrl__StrictMode).ToLocalFile()
	var protoFiles []string

	// TODO [RC] We should do the search async and show a loading/searching icon to the user
	filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if filepath.Ext(path) == ".proto" {
			protoFiles = append(protoFiles, path)
		}
		return nil
	})

	if len(protoFiles) == 0 {
		// TODO [RC] Show error to user that there is no proto files found
		return
	}

	// TODO [RC] Shoud we be replacing or adding?
	c.Options().ProtoListModel().SetStringList(protoFiles)
}

func (c *workspaceController) addImport(path string) {
	path = core.NewQUrl3(path, core.QUrl__StrictMode).ToLocalFile()
	lm := c.Options().ImportListModel()
	for _, p := range lm.StringList() {
		if p == path {
			return
		}
	}
	lm.SetStringList(append(lm.StringList(), path))
}

func (c *workspaceController) processProtos() error {
	imports := c.Options().ImportListModel().StringList()
	protos := c.Options().ProtoListModel().StringList()
	return c.InputCtrl().processProtos(imports, protos)
}

func (c *workspaceController) connect(addr string) error {
	if addr == "" {
		return errors.New("no address to connect")
	}

	if c.Options().Addr() == addr && c.grpcConn != nil {
		return nil
	}

	if c.grpcConn != nil {
		c.grpcConn.Close()
		c.cancelCtxFunc()
		c.grpcConn = nil
	}

	var err error
	c.grpcConn, err = BlockDial(addr, c.Options(), c.OutputCtrl())
	if err != nil {
		return err
	}

	var ctx context.Context
	ctx, c.cancelCtxFunc = context.WithCancel(context.Background())

	go func() {
		for {
			if c.grpcConn == nil {
				c.SetConnState(connectivity.Shutdown.String())
				break

			}
			state := c.grpcConn.GetState()
			c.SetConnState(state.String())
			if ok := c.grpcConn.WaitForStateChange(ctx, state); !ok {
				break
			}
		}
	}()

	c.Options().SetAddr(addr)

	go func() {
		opts := c.Options()
		w := &db.Workspace{
			Addr:        addr,
			Reflect:     opts.IsReflect(),
			Insecure:    opts.IsInsecure(),
			Plaintext:   opts.IsPlaintext(),
			Rootca:      opts.Rootca(),
			Clientcert:  opts.Clientcert(),
			Clientkey:   opts.Clientkey(),
			ProtoFiles:  opts.ProtoListModel().StringList(),
			ImportFiles: opts.ImportListModel().StringList(),
		}
		c.store.Put(w)
	}()
	return nil
}

func (c *workspaceController) send(service, method string) error {
	if c.grpcConn == nil {
		return nil
	}

	md := c.InputCtrl().pbSource.GetMethodDesc(service, method)
	req := processMessage(c.InputCtrl().RequestModel())

	meta := make(map[string]string)
	for _, kv := range c.InputCtrl().MetadataListModel().List() {
		if kv.Key() == "" {
			continue
		}
		meta[kv.Key()] = kv.Val()
	}

	return c.OutputCtrl().invokeMethod(c.grpcConn, md, req, meta)
}

func processMessage(msg *model.Message) *dynamic.Message {
	dm := dynamic.NewMessage(msg.Ref)
	for _, f := range msg.Fields() {
		switch f.FdType {
		case descriptor.FieldDescriptorProto_TYPE_MESSAGE:
			if f.IsRepeated {
				var fields []interface{}
				for _, v := range f.ValueListModel().Values() {
					fields = append(fields, processMessage(v.MsgValue()))
				}
				dm.SetFieldByNumber(f.Tag(), fields)
				break
			}
			dm.SetFieldByNumber(f.Tag(), processMessage(f.Message()))

		default:
			if f.IsRepeated {
				var fields []interface{}
				for _, v := range f.ValueListModel().Values() {
					fields = append(fields, parseStringValue(f.FdType, v.Value()))
				}
				dm.SetFieldByNumber(f.Tag(), fields)
				break
			}
			dm.SetFieldByNumber(f.Tag(), parseStringValue(f.FdType, f.Value()))
		}
	}

	return dm
}

func parseStringValue(fdType descriptor.FieldDescriptorProto_Type, val string) interface{} {
	switch fdType {
	case descriptor.FieldDescriptorProto_TYPE_DOUBLE:
		v, _ := strconv.ParseFloat(val, 64)
		return v
	case descriptor.FieldDescriptorProto_TYPE_FLOAT:
		v, _ := strconv.ParseFloat(val, 32)
		return float32(v)
	case descriptor.FieldDescriptorProto_TYPE_INT32,
		descriptor.FieldDescriptorProto_TYPE_SINT32,
		descriptor.FieldDescriptorProto_TYPE_SFIXED32,
		descriptor.FieldDescriptorProto_TYPE_ENUM:
		v, _ := strconv.ParseInt(val, 10, 32)
		return int32(v)
	case descriptor.FieldDescriptorProto_TYPE_INT64,
		descriptor.FieldDescriptorProto_TYPE_SINT64,
		descriptor.FieldDescriptorProto_TYPE_SFIXED64:
		v, _ := strconv.ParseInt(val, 10, 64)
		return v
	case descriptor.FieldDescriptorProto_TYPE_UINT32,
		descriptor.FieldDescriptorProto_TYPE_FIXED32:
		v, _ := strconv.ParseUint(val, 10, 32)
		return uint32(v)
	case descriptor.FieldDescriptorProto_TYPE_UINT64,
		descriptor.FieldDescriptorProto_TYPE_FIXED64:
		v, _ := strconv.ParseUint(val, 10, 64)
		return v
	case descriptor.FieldDescriptorProto_TYPE_BOOL:
		v, _ := strconv.ParseBool(val)
		return v
	case descriptor.FieldDescriptorProto_TYPE_BYTES:
		return []byte(val)
	default:
		return val
	}
}
