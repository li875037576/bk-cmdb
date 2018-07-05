/*
 * Tencent is pleased to support the open source community by making 蓝鲸 available.
 * Copyright (C) 2017-2018 THL A29 Limited, a Tencent company. All rights reserved.
 * Licensed under the MIT License (the "License"); you may not use this file except
 * in compliance with the License. You may obtain a copy of the License at
 * http://opensource.org/licenses/MIT
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions and
 * limitations under the License.
 */

package service

import (
	"configcenter/src/scene_server/topo_server/core/supplementary"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"

	"github.com/emicklei/go-restful"

	apiutil "configcenter/src/apimachinery/util"
	"configcenter/src/common"
	"configcenter/src/common/blog"
	"configcenter/src/common/core/cc/api"
	"configcenter/src/common/errors"
	"configcenter/src/common/http/httpserver"
	"configcenter/src/common/language"
	frtypes "configcenter/src/common/mapstr"
	"configcenter/src/common/util"
	"configcenter/src/scene_server/topo_server/app/options"
	"configcenter/src/scene_server/topo_server/core"
	"configcenter/src/scene_server/topo_server/core/types"
)

// TopoServiceInterface the topo service methods used to init
type TopoServiceInterface interface {
	SetOperation(operation core.Core, err errors.CCErrorIf, language language.CCLanguageIf)
	WebService(filter restful.FilterFunction) *restful.WebService
	SetConfig(cfg options.Config)
}

// New ceate topo servcie instance
func New() TopoServiceInterface {
	return &topoService{}
}

// topoService topo service
type topoService struct {
	language language.CCLanguageIf
	err      errors.CCErrorIf
	actions  []action
	core     core.Core
	cfg      options.Config
}

func (s *topoService) SetConfig(cfg options.Config) {
	s.cfg = cfg
}

// SetOperation set the operation
func (s *topoService) SetOperation(operation core.Core, err errors.CCErrorIf, language language.CCLanguageIf) {

	s.core = operation
	s.err = err
	s.language = language

}

// WebService the web service
func (s *topoService) WebService(filter restful.FilterFunction) *restful.WebService {

	// init service actions
	s.initService()

	ws := new(restful.WebService)
	//ws.Path("/topo/v3").Filter(filter).Produces(restful.MIME_JSON).Consumes(restful.MIME_JSON)
	ws.Path("/topo/v3").Produces(restful.MIME_JSON).Consumes(restful.MIME_JSON)

	innerActions := s.Actions()

	for _, actionItem := range innerActions {
		switch actionItem.Verb {
		case http.MethodPost:
			ws.Route(ws.POST(actionItem.Path).To(actionItem.Handler))
		case http.MethodDelete:
			ws.Route(ws.DELETE(actionItem.Path).To(actionItem.Handler))
		case http.MethodPut:
			ws.Route(ws.PUT(actionItem.Path).To(actionItem.Handler))
		case http.MethodGet:
			ws.Route(ws.GET(actionItem.Path).To(actionItem.Handler))
		default:
			blog.Errorf(" the url (%s), the http method (%s) is not supported", actionItem.Path, actionItem.Verb)
		}
	}

	return ws
}

func (s *topoService) createAPIRspStr(errcode int, info interface{}) (string, error) {
	rsp := api.BKAPIRsp{
		Result:  true,
		Code:    0,
		Message: nil,
		Data:    nil,
	}

	if common.CCSuccess != errcode {
		rsp.Result = false
		rsp.Code = errcode
		rsp.Message = info
	} else {
		rsp.Message = common.CCSuccessStr
		rsp.Data = info
	}

	data, err := json.Marshal(rsp)
	return string(data), err
}

func (s *topoService) sendResponse(resp *restful.Response, dataMsg interface{}) {
	resp.Header().Set("Content-Type", "application/json")
	if rsp, rspErr := s.createAPIRspStr(common.CCSuccess, dataMsg); nil == rspErr {
		io.WriteString(resp, rsp)
	}
}

// Actions return the all actions
func (s *topoService) Actions() []*httpserver.Action {

	var httpactions []*httpserver.Action
	for _, a := range s.actions {

		func(act action) {

			httpactions = append(httpactions, &httpserver.Action{Verb: act.Method, Path: act.Path, Handler: func(req *restful.Request, resp *restful.Response) {

				ownerID := util.GetActionOnwerID(req)
				user := util.GetActionUser(req)

				// get the language
				language := util.GetActionLanguage(req)

				defLang := s.language.CreateDefaultCCLanguageIf(language)

				// get the error info by the language
				defErr := s.err.CreateDefaultCCErrorIf(language)

				value, err := ioutil.ReadAll(req.Request.Body)
				if err != nil {
					blog.Errorf("read http request body failed, error:%s", err.Error())
					errStr := defErr.Error(common.CCErrCommHTTPReadBodyFailed)
					respData, _ := s.createAPIRspStr(common.CCErrCommHTTPReadBodyFailed, errStr)
					s.sendResponse(resp, respData)
					return
				}

				mData := frtypes.MapStr{}
				if err := json.Unmarshal(value, &mData); nil != err {
					blog.Errorf("failed to unmarshal the data, error %s", err.Error())
					errStr := defErr.Error(common.CCErrCommJSONUnmarshalFailed)
					respData, _ := s.createAPIRspStr(common.CCErrCommJSONUnmarshalFailed, errStr)
					s.sendResponse(resp, respData)
					return
				}

				data, dataErr := act.HandlerFunc(types.ContextParams{
					Support: supplementary.New(),
					Err:     defErr,
					Lang:    defLang,
					Header: apiutil.Headers{
						Language: language,
						User:     user,
						OwnerID:  ownerID,
					},
				},
					req.PathParameter,
					req.QueryParameter,
					mData)

				if nil != dataErr {
					blog.Errorf("%s", dataErr.Error())
					switch e := dataErr.(type) {
					default:
						respData, _ := s.createAPIRspStr(common.CCSystemBusy, dataErr.Error())
						s.sendResponse(resp, respData)
					case errors.CCErrorCoder:
						respData, _ := s.createAPIRspStr(e.GetCode(), dataErr.Error())
						s.sendResponse(resp, respData)
					}
					return
				}

				s.sendResponse(resp, data)

			}})
		}(a)

	}
	return httpactions
}
