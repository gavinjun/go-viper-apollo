package apollo

import (
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"io/ioutil"
)

type Notification struct {
	NamespaceName string `json:"namespaceName"`
	NotificationId int `json:"notificationId"`
}
type NocacheResponse struct {
	AppId string `json:"appId"`
	Cluster string `json:"cluster"`
	NamespaceName string `json:"namespaceName"`
	Configurations interface{} `json:"configurations"`
	releaseKey string `json:"releaseKey"`
}


// 请求无缓存接口
// URL: {config_server_url}/configs/{appId}/{clusterName}/{namespaceName}?releaseKey={releaseKey}&ip={clientIp}
func NocacheGet(pollTask *PollTask, nameSpaceName string, notificationId int) (*WatchEvent, error) {
	requestURL := fmt.Sprintf("%s/configs/%s/%s/%s", pollTask.Config.ConfigServerUrl, pollTask.Config.AppId, pollTask.Config.ClusterName, nameSpaceName)
	response, error := pollTask.HttpRequest.Request(requestURL)
	if error != nil {
		return nil, errors.WithMessage(error, "notifications 请求notifications失败")
	}
	// 判断http状态
	if response.StatusCode == 200 {
		body,err := ioutil.ReadAll(response.Body)
		if err != nil {
			return nil, errors.WithMessage(err, "notifications 请求notifications失败")
		}
		nocacheResp := NocacheResponse{}
		if unmarshalErr := json.Unmarshal(body, &nocacheResp); unmarshalErr != nil {
			return nil, errors.WithMessagef(unmarshalErr, "获取无缓存接口 返回格式错误：%s", string(body))
		}
		byteSlice,marshalErr := json.Marshal(nocacheResp.Configurations);
		if marshalErr != nil {
			return nil, errors.WithMessagef(err, "获取无缓存接口 返回格式错误：%s", string(body))
		}
		watchEvent := WatchEvent{
			NamespaceName: nameSpaceName,
			Bytes: byteSlice,
		}
		return &watchEvent, nil
	}
	return nil, nil
}

// 请求nameSpace是否有变化
// URL: {config_server_url}/notifications/v2?appId={appId}&cluster={clusterName}&notifications={notifications}
func NotificationsGet(pollTask *PollTask, isInit bool) error {
	requestURL := fmt.Sprintf("%s/notifications/v2?appId=%s&cluster=%s&notifications=", pollTask.Config.ConfigServerUrl, pollTask.Config.AppId, pollTask.Config.ClusterName)
	nameSpaceSlice := make([]Notification, 0)
	pollTask.Mutex.Lock()
	defer pollTask.Mutex.Unlock()
	for key, value := range pollTask.namespaceNames {
		nameSpaceSlice = append(nameSpaceSlice, Notification{
			NamespaceName:  key,
			NotificationId: value,
		})
	}
	nameSpaceSliceJson, err := json.Marshal(nameSpaceSlice)
	if err != nil {
		return errors.WithMessage(err, "notifications 请求设置本地notifications信息 json序列化失败")
	}
	requestURL = requestURL + string(nameSpaceSliceJson)
	response, error := pollTask.HttpRequest.Request(requestURL)
	if error != nil {
		pollTask.logger.Error(error.Error())
		return errors.WithMessage(error, "notifications 请求notifications失败")
	}
	if response.StatusCode == 304 {
		pollTask.logger.Info("notifications请求304")
	}
	// 判断http状态
	if response.StatusCode == 200 {
		body,err := ioutil.ReadAll(response.Body)
		if err != nil {
			return errors.WithMessage(err, "notifications 请求notifications失败")
		}
		var rtNameSpaceSlice []*Notification

		if unmarshalErr := json.Unmarshal(body, &rtNameSpaceSlice); unmarshalErr != nil {
			return errors.WithMessagef(unmarshalErr, "notifications 返回格式错误：%s", string(body))
		}

		// 处理配置变化的方法
		// 批量发出更新事件
		pollTask.logger.Info("获取最新配置")
		if isInit {
			// 判断配置中的namespace是否在apollo不存在
			if len(rtNameSpaceSlice) != len(pollTask.namespaceNames) {
				return errors.New("apollo 未获取到期望的namespace， 检查配置viper.remoteprovider.apollo.namespaceNames")
			}
		}
		var startEvents  = make([]*WatchEvent, 0, len(rtNameSpaceSlice))
		for _, value := range rtNameSpaceSlice {
			// 循环构建一个eventList数据包过去
			watchEvent, errGet := NocacheGet(pollTask, value.NamespaceName, value.NotificationId)
			if errGet != nil {
				pollTask.logger.Error(errGet.Error())
			}
			if watchEvent!=nil {
				if isInit {
					startEvents = append(startEvents, watchEvent)
				} else {
					pollTask.bus.Publish(ApolloLongPollTopic, *watchEvent)
				}
				pollTask.namespaceNames[value.NamespaceName] = value.NotificationId
			}
		}
		if isInit && len(startEvents) > 0 {
			// 组装批量事件
			pollTask.bus.Publish(ApolloFirstPollTopic, startEvents)
		}

	}
	return nil
}



