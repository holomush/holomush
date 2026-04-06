# Protocol Documentation

<a name="top"></a>

## Table of Contents

- [holomush/core/v1/core.proto](#holomush_core_v1_core-proto)
    - [AuthenticatePlayerRequest](#holomush-core-v1-AuthenticatePlayerRequest)
    - [AuthenticatePlayerResponse](#holomush-core-v1-AuthenticatePlayerResponse)
    - [CharacterSummary](#holomush-core-v1-CharacterSummary)
    - [CheckPlayerSessionRequest](#holomush-core-v1-CheckPlayerSessionRequest)
    - [CheckPlayerSessionResponse](#holomush-core-v1-CheckPlayerSessionResponse)
    - [ConfirmPasswordResetRequest](#holomush-core-v1-ConfirmPasswordResetRequest)
    - [ConfirmPasswordResetResponse](#holomush-core-v1-ConfirmPasswordResetResponse)
    - [ControlFrame](#holomush-core-v1-ControlFrame)
    - [CreateCharacterRequest](#holomush-core-v1-CreateCharacterRequest)
    - [CreateCharacterResponse](#holomush-core-v1-CreateCharacterResponse)
    - [CreateGuestRequest](#holomush-core-v1-CreateGuestRequest)
    - [CreateGuestResponse](#holomush-core-v1-CreateGuestResponse)
    - [CreatePlayerRequest](#holomush-core-v1-CreatePlayerRequest)
    - [CreatePlayerResponse](#holomush-core-v1-CreatePlayerResponse)
    - [DisconnectRequest](#holomush-core-v1-DisconnectRequest)
    - [DisconnectResponse](#holomush-core-v1-DisconnectResponse)
    - [EventFrame](#holomush-core-v1-EventFrame)
    - [GetCommandHistoryRequest](#holomush-core-v1-GetCommandHistoryRequest)
    - [GetCommandHistoryResponse](#holomush-core-v1-GetCommandHistoryResponse)
    - [HandleCommandRequest](#holomush-core-v1-HandleCommandRequest)
    - [HandleCommandResponse](#holomush-core-v1-HandleCommandResponse)
    - [ListCharactersRequest](#holomush-core-v1-ListCharactersRequest)
    - [ListCharactersResponse](#holomush-core-v1-ListCharactersResponse)
    - [LogoutRequest](#holomush-core-v1-LogoutRequest)
    - [LogoutResponse](#holomush-core-v1-LogoutResponse)
    - [RequestMeta](#holomush-core-v1-RequestMeta)
    - [RequestPasswordResetRequest](#holomush-core-v1-RequestPasswordResetRequest)
    - [RequestPasswordResetResponse](#holomush-core-v1-RequestPasswordResetResponse)
    - [ResponseMeta](#holomush-core-v1-ResponseMeta)
    - [SelectCharacterRequest](#holomush-core-v1-SelectCharacterRequest)
    - [SelectCharacterResponse](#holomush-core-v1-SelectCharacterResponse)
    - [SubscribeRequest](#holomush-core-v1-SubscribeRequest)
    - [SubscribeResponse](#holomush-core-v1-SubscribeResponse)

    - [ControlSignal](#holomush-core-v1-ControlSignal)

    - [CoreService](#holomush-core-v1-CoreService)

- [holomush/web/v1/web.proto](#holomush_web_v1_web-proto)
    - [CharacterSummary](#holomush-web-v1-CharacterSummary)
    - [ControlFrame](#holomush-web-v1-ControlFrame)
    - [DisconnectRequest](#holomush-web-v1-DisconnectRequest)
    - [DisconnectResponse](#holomush-web-v1-DisconnectResponse)
    - [GameEvent](#holomush-web-v1-GameEvent)
    - [GetCommandHistoryRequest](#holomush-web-v1-GetCommandHistoryRequest)
    - [GetCommandHistoryResponse](#holomush-web-v1-GetCommandHistoryResponse)
    - [SendCommandRequest](#holomush-web-v1-SendCommandRequest)
    - [SendCommandResponse](#holomush-web-v1-SendCommandResponse)
    - [StreamEventsRequest](#holomush-web-v1-StreamEventsRequest)
    - [StreamEventsResponse](#holomush-web-v1-StreamEventsResponse)
    - [WebAuthenticatePlayerRequest](#holomush-web-v1-WebAuthenticatePlayerRequest)
    - [WebAuthenticatePlayerResponse](#holomush-web-v1-WebAuthenticatePlayerResponse)
    - [WebCheckSessionRequest](#holomush-web-v1-WebCheckSessionRequest)
    - [WebCheckSessionResponse](#holomush-web-v1-WebCheckSessionResponse)
    - [WebConfirmPasswordResetRequest](#holomush-web-v1-WebConfirmPasswordResetRequest)
    - [WebConfirmPasswordResetResponse](#holomush-web-v1-WebConfirmPasswordResetResponse)
    - [WebContentItem](#holomush-web-v1-WebContentItem)
    - [WebContentItem.MetadataEntry](#holomush-web-v1-WebContentItem-MetadataEntry)
    - [WebCreateCharacterRequest](#holomush-web-v1-WebCreateCharacterRequest)
    - [WebCreateCharacterResponse](#holomush-web-v1-WebCreateCharacterResponse)
    - [WebCreateGuestRequest](#holomush-web-v1-WebCreateGuestRequest)
    - [WebCreateGuestResponse](#holomush-web-v1-WebCreateGuestResponse)
    - [WebCreatePlayerRequest](#holomush-web-v1-WebCreatePlayerRequest)
    - [WebCreatePlayerResponse](#holomush-web-v1-WebCreatePlayerResponse)
    - [WebGetContentRequest](#holomush-web-v1-WebGetContentRequest)
    - [WebGetContentResponse](#holomush-web-v1-WebGetContentResponse)
    - [WebListCharactersRequest](#holomush-web-v1-WebListCharactersRequest)
    - [WebListCharactersResponse](#holomush-web-v1-WebListCharactersResponse)
    - [WebListContentRequest](#holomush-web-v1-WebListContentRequest)
    - [WebListContentResponse](#holomush-web-v1-WebListContentResponse)
    - [WebLogoutRequest](#holomush-web-v1-WebLogoutRequest)
    - [WebLogoutResponse](#holomush-web-v1-WebLogoutResponse)
    - [WebRequestPasswordResetRequest](#holomush-web-v1-WebRequestPasswordResetRequest)
    - [WebRequestPasswordResetResponse](#holomush-web-v1-WebRequestPasswordResetResponse)
    - [WebSelectCharacterRequest](#holomush-web-v1-WebSelectCharacterRequest)
    - [WebSelectCharacterResponse](#holomush-web-v1-WebSelectCharacterResponse)

    - [ControlSignal](#holomush-web-v1-ControlSignal)
    - [EventChannel](#holomush-web-v1-EventChannel)

    - [WebService](#holomush-web-v1-WebService)

- [holomush/control/v1/control.proto](#holomush_control_v1_control-proto)
    - [ShutdownRequest](#holomush-control-v1-ShutdownRequest)
    - [ShutdownResponse](#holomush-control-v1-ShutdownResponse)
    - [StatusRequest](#holomush-control-v1-StatusRequest)
    - [StatusResponse](#holomush-control-v1-StatusResponse)

    - [ControlService](#holomush-control-v1-ControlService)

- [holomush/plugin/v1/plugin.proto](#holomush_plugin_v1_plugin-proto)
    - [CommandRequest](#holomush-plugin-v1-CommandRequest)
    - [CommandResponse](#holomush-plugin-v1-CommandResponse)
    - [EmitEvent](#holomush-plugin-v1-EmitEvent)
    - [Event](#holomush-plugin-v1-Event)
    - [HandleCommandRequest](#holomush-plugin-v1-HandleCommandRequest)
    - [HandleCommandResponse](#holomush-plugin-v1-HandleCommandResponse)
    - [HandleEventRequest](#holomush-plugin-v1-HandleEventRequest)
    - [HandleEventResponse](#holomush-plugin-v1-HandleEventResponse)
    - [InitRequest](#holomush-plugin-v1-InitRequest)
    - [InitResponse](#holomush-plugin-v1-InitResponse)
    - [PluginHostServiceEmitEventRequest](#holomush-plugin-v1-PluginHostServiceEmitEventRequest)
    - [PluginHostServiceEmitEventResponse](#holomush-plugin-v1-PluginHostServiceEmitEventResponse)
    - [PluginHostServiceKVDeleteRequest](#holomush-plugin-v1-PluginHostServiceKVDeleteRequest)
    - [PluginHostServiceKVDeleteResponse](#holomush-plugin-v1-PluginHostServiceKVDeleteResponse)
    - [PluginHostServiceKVGetRequest](#holomush-plugin-v1-PluginHostServiceKVGetRequest)
    - [PluginHostServiceKVGetResponse](#holomush-plugin-v1-PluginHostServiceKVGetResponse)
    - [PluginHostServiceKVSetRequest](#holomush-plugin-v1-PluginHostServiceKVSetRequest)
    - [PluginHostServiceKVSetResponse](#holomush-plugin-v1-PluginHostServiceKVSetResponse)
    - [PluginHostServiceLogRequest](#holomush-plugin-v1-PluginHostServiceLogRequest)
    - [PluginHostServiceLogResponse](#holomush-plugin-v1-PluginHostServiceLogResponse)
    - [ServiceConfig](#holomush-plugin-v1-ServiceConfig)
    - [ServiceConfig.RequiredServicesEntry](#holomush-plugin-v1-ServiceConfig-RequiredServicesEntry)

    - [CommandStatus](#holomush-plugin-v1-CommandStatus)

    - [PluginHostService](#holomush-plugin-v1-PluginHostService)
    - [PluginService](#holomush-plugin-v1-PluginService)

- [holomush/plugin/v1/hostfunc.proto](#holomush_plugin_v1_hostfunc-proto)
    - [CharacterInfo](#holomush-plugin-v1-CharacterInfo)
    - [CommandHelpInfo](#holomush-plugin-v1-CommandHelpInfo)
    - [CommandInfo](#holomush-plugin-v1-CommandInfo)
    - [EmitEventRequest](#holomush-plugin-v1-EmitEventRequest)
    - [EmitEventResponse](#holomush-plugin-v1-EmitEventResponse)
    - [GetCommandHelpRequest](#holomush-plugin-v1-GetCommandHelpRequest)
    - [GetCommandHelpResponse](#holomush-plugin-v1-GetCommandHelpResponse)
    - [KVDeleteRequest](#holomush-plugin-v1-KVDeleteRequest)
    - [KVDeleteResponse](#holomush-plugin-v1-KVDeleteResponse)
    - [KVGetRequest](#holomush-plugin-v1-KVGetRequest)
    - [KVGetResponse](#holomush-plugin-v1-KVGetResponse)
    - [KVSetRequest](#holomush-plugin-v1-KVSetRequest)
    - [KVSetResponse](#holomush-plugin-v1-KVSetResponse)
    - [ListCommandsRequest](#holomush-plugin-v1-ListCommandsRequest)
    - [ListCommandsResponse](#holomush-plugin-v1-ListCommandsResponse)
    - [LocationInfo](#holomush-plugin-v1-LocationInfo)
    - [LogRequest](#holomush-plugin-v1-LogRequest)
    - [LogRequest.FieldsEntry](#holomush-plugin-v1-LogRequest-FieldsEntry)
    - [LogResponse](#holomush-plugin-v1-LogResponse)
    - [QueryCharacterRequest](#holomush-plugin-v1-QueryCharacterRequest)
    - [QueryCharacterResponse](#holomush-plugin-v1-QueryCharacterResponse)
    - [QueryLocationCharactersRequest](#holomush-plugin-v1-QueryLocationCharactersRequest)
    - [QueryLocationCharactersResponse](#holomush-plugin-v1-QueryLocationCharactersResponse)
    - [QueryLocationRequest](#holomush-plugin-v1-QueryLocationRequest)
    - [QueryLocationResponse](#holomush-plugin-v1-QueryLocationResponse)

    - [LogLevel](#holomush-plugin-v1-LogLevel)

    - [HostFunctionsService](#holomush-plugin-v1-HostFunctionsService)

- [Scalar Value Types](#scalar-value-types)

<a name="holomush_core_v1_core-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## holomush/core/v1/core.proto

<a name="holomush-core-v1-AuthenticatePlayerRequest"></a>

### AuthenticatePlayerRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| username | [string](#string) |  |  |
| password | [string](#string) |  |  |
| captcha_token | [string](#string) |  |  |
| remember_me | [bool](#bool) |  |  |

<a name="holomush-core-v1-AuthenticatePlayerResponse"></a>

### AuthenticatePlayerResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| player_session_token | [string](#string) |  |  |
| error_message | [string](#string) |  |  |
| characters | [CharacterSummary](#holomush-core-v1-CharacterSummary) | repeated |  |
| default_character_id | [string](#string) |  |  |

<a name="holomush-core-v1-CharacterSummary"></a>

### CharacterSummary

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  |  |
| character_name | [string](#string) |  |  |
| has_active_session | [bool](#bool) |  |  |
| session_status | [string](#string) |  |  |
| last_location | [string](#string) |  |  |
| last_played_at | [int64](#int64) |  |  |

<a name="holomush-core-v1-CheckPlayerSessionRequest"></a>

### CheckPlayerSessionRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| player_session_token | [string](#string) |  |  |

<a name="holomush-core-v1-CheckPlayerSessionResponse"></a>

### CheckPlayerSessionResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| player_name | [string](#string) |  |  |

<a name="holomush-core-v1-ConfirmPasswordResetRequest"></a>

### ConfirmPasswordResetRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| token | [string](#string) |  |  |
| new_password | [string](#string) |  |  |

<a name="holomush-core-v1-ConfirmPasswordResetResponse"></a>

### ConfirmPasswordResetResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| error_message | [string](#string) |  |  |

<a name="holomush-core-v1-ControlFrame"></a>

### ControlFrame

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| signal | [ControlSignal](#holomush-core-v1-ControlSignal) |  |  |
| message | [string](#string) |  |  |

<a name="holomush-core-v1-CreateCharacterRequest"></a>

### CreateCharacterRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| player_session_token | [string](#string) |  |  |
| character_name | [string](#string) |  |  |

<a name="holomush-core-v1-CreateCharacterResponse"></a>

### CreateCharacterResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| character_id | [string](#string) |  |  |
| character_name | [string](#string) |  |  |
| error_message | [string](#string) |  |  |

<a name="holomush-core-v1-CreateGuestRequest"></a>

### CreateGuestRequest

<a name="holomush-core-v1-CreateGuestResponse"></a>

### CreateGuestResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| error_message | [string](#string) |  |  |
| player_session_token | [string](#string) |  |  |
| characters | [CharacterSummary](#holomush-core-v1-CharacterSummary) | repeated |  |
| default_character_id | [string](#string) |  |  |

<a name="holomush-core-v1-CreatePlayerRequest"></a>

### CreatePlayerRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| username | [string](#string) |  |  |
| password | [string](#string) |  |  |
| email | [string](#string) |  |  |
| captcha_token | [string](#string) |  |  |

<a name="holomush-core-v1-CreatePlayerResponse"></a>

### CreatePlayerResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| player_session_token | [string](#string) |  |  |
| characters | [CharacterSummary](#holomush-core-v1-CharacterSummary) | repeated |  |
| error_message | [string](#string) |  |  |

<a name="holomush-core-v1-DisconnectRequest"></a>

### DisconnectRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [RequestMeta](#holomush-core-v1-RequestMeta) |  |  |
| session_id | [string](#string) |  |  |
| connection_id | [string](#string) |  | optional: remove specific connection |

<a name="holomush-core-v1-DisconnectResponse"></a>

### DisconnectResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [ResponseMeta](#holomush-core-v1-ResponseMeta) |  |  |
| success | [bool](#bool) |  |  |

<a name="holomush-core-v1-EventFrame"></a>

### EventFrame

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
| stream | [string](#string) |  |  |
| type | [string](#string) |  |  |
| timestamp | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  |  |
| actor_type | [string](#string) |  |  |
| actor_id | [string](#string) |  |  |
| payload | [bytes](#bytes) |  |  |

<a name="holomush-core-v1-GetCommandHistoryRequest"></a>

### GetCommandHistoryRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [RequestMeta](#holomush-core-v1-RequestMeta) |  |  |
| session_id | [string](#string) |  |  |

<a name="holomush-core-v1-GetCommandHistoryResponse"></a>

### GetCommandHistoryResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [ResponseMeta](#holomush-core-v1-ResponseMeta) |  |  |
| success | [bool](#bool) |  |  |
| commands | [string](#string) | repeated |  |
| error | [string](#string) |  |  |

<a name="holomush-core-v1-HandleCommandRequest"></a>

### HandleCommandRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [RequestMeta](#holomush-core-v1-RequestMeta) |  |  |
| session_id | [string](#string) |  |  |
| command | [string](#string) |  |  |

<a name="holomush-core-v1-HandleCommandResponse"></a>

### HandleCommandResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [ResponseMeta](#holomush-core-v1-ResponseMeta) |  |  |
| success | [bool](#bool) |  |  |
| error | [string](#string) |  |  |

<a name="holomush-core-v1-ListCharactersRequest"></a>

### ListCharactersRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| player_session_token | [string](#string) |  |  |

<a name="holomush-core-v1-ListCharactersResponse"></a>

### ListCharactersResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| characters | [CharacterSummary](#holomush-core-v1-CharacterSummary) | repeated |  |

<a name="holomush-core-v1-LogoutRequest"></a>

### LogoutRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| player_session_token | [string](#string) |  |  |

<a name="holomush-core-v1-LogoutResponse"></a>

### LogoutResponse

<a name="holomush-core-v1-RequestMeta"></a>

### RequestMeta

RequestMeta contains metadata for request correlation and debugging.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| request_id | [string](#string) |  | ULID for log correlation |
| timestamp | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  |  |

<a name="holomush-core-v1-RequestPasswordResetRequest"></a>

### RequestPasswordResetRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| email | [string](#string) |  |  |

<a name="holomush-core-v1-RequestPasswordResetResponse"></a>

### RequestPasswordResetResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |

<a name="holomush-core-v1-ResponseMeta"></a>

### ResponseMeta

ResponseMeta contains metadata echoed back from requests.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| request_id | [string](#string) |  | Echoed from request |
| timestamp | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) |  |  |

<a name="holomush-core-v1-SelectCharacterRequest"></a>

### SelectCharacterRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| player_session_token | [string](#string) |  |  |
| character_id | [string](#string) |  |  |

<a name="holomush-core-v1-SelectCharacterResponse"></a>

### SelectCharacterResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| session_id | [string](#string) |  |  |
| character_name | [string](#string) |  |  |
| reattached | [bool](#bool) |  |  |
| error_message | [string](#string) |  |  |

<a name="holomush-core-v1-SubscribeRequest"></a>

### SubscribeRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta | [RequestMeta](#holomush-core-v1-RequestMeta) |  |  |
| session_id | [string](#string) |  |  |
| streams | [string](#string) | repeated |  |
| replay_from_cursor | [bool](#bool) |  |  |

<a name="holomush-core-v1-SubscribeResponse"></a>

### SubscribeResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| event | [EventFrame](#holomush-core-v1-EventFrame) |  |  |
| control | [ControlFrame](#holomush-core-v1-ControlFrame) |  |  |

<a name="holomush-core-v1-ControlSignal"></a>

### ControlSignal

| Name | Number | Description |
| ---- | ------ | ----------- |
| CONTROL_SIGNAL_UNSPECIFIED | 0 |  |
| CONTROL_SIGNAL_REPLAY_COMPLETE | 1 |  |
| CONTROL_SIGNAL_STREAM_CLOSED | 2 |  |

<a name="holomush-core-v1-CoreService"></a>

### CoreService

CoreService is the main game service.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| HandleCommand | [HandleCommandRequest](#holomush-core-v1-HandleCommandRequest) | [HandleCommandResponse](#holomush-core-v1-HandleCommandResponse) | HandleCommand processes a game command. |
| Subscribe | [SubscribeRequest](#holomush-core-v1-SubscribeRequest) | [SubscribeResponse](#holomush-core-v1-SubscribeResponse) stream | Subscribe opens a stream of events for the session. |
| Disconnect | [DisconnectRequest](#holomush-core-v1-DisconnectRequest) | [DisconnectResponse](#holomush-core-v1-DisconnectResponse) | Disconnect ends a session. |
| GetCommandHistory | [GetCommandHistoryRequest](#holomush-core-v1-GetCommandHistoryRequest) | [GetCommandHistoryResponse](#holomush-core-v1-GetCommandHistoryResponse) | GetCommandHistory retrieves command history for a session. |
| AuthenticatePlayer | [AuthenticatePlayerRequest](#holomush-core-v1-AuthenticatePlayerRequest) | [AuthenticatePlayerResponse](#holomush-core-v1-AuthenticatePlayerResponse) | Two-phase login: authenticate player credentials. |
| SelectCharacter | [SelectCharacterRequest](#holomush-core-v1-SelectCharacterRequest) | [SelectCharacterResponse](#holomush-core-v1-SelectCharacterResponse) | Two-phase login: select a character, creating or reattaching a game session. |
| CreatePlayer | [CreatePlayerRequest](#holomush-core-v1-CreatePlayerRequest) | [CreatePlayerResponse](#holomush-core-v1-CreatePlayerResponse) | Create a new player account. |
| CreateGuest | [CreateGuestRequest](#holomush-core-v1-CreateGuestRequest) | [CreateGuestResponse](#holomush-core-v1-CreateGuestResponse) | Create an ephemeral guest player and character. |
| CreateCharacter | [CreateCharacterRequest](#holomush-core-v1-CreateCharacterRequest) | [CreateCharacterResponse](#holomush-core-v1-CreateCharacterResponse) | Create a new character for an authenticated player. |
| ListCharacters | [ListCharactersRequest](#holomush-core-v1-ListCharactersRequest) | [ListCharactersResponse](#holomush-core-v1-ListCharactersResponse) | List characters for an authenticated player. |
| RequestPasswordReset | [RequestPasswordResetRequest](#holomush-core-v1-RequestPasswordResetRequest) | [RequestPasswordResetResponse](#holomush-core-v1-RequestPasswordResetResponse) | Request a password reset (email stubbed). |
| ConfirmPasswordReset | [ConfirmPasswordResetRequest](#holomush-core-v1-ConfirmPasswordResetRequest) | [ConfirmPasswordResetResponse](#holomush-core-v1-ConfirmPasswordResetResponse) | Confirm a password reset with token. |
| Logout | [LogoutRequest](#holomush-core-v1-LogoutRequest) | [LogoutResponse](#holomush-core-v1-LogoutResponse) | End a player session. |
| CheckPlayerSession | [CheckPlayerSessionRequest](#holomush-core-v1-CheckPlayerSessionRequest) | [CheckPlayerSessionResponse](#holomush-core-v1-CheckPlayerSessionResponse) | Validate a player session token. Used by web gateway for cookie-based auth checks. |

<a name="holomush_web_v1_web-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## holomush/web/v1/web.proto

<a name="holomush-web-v1-CharacterSummary"></a>

### CharacterSummary

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  |  |
| character_name | [string](#string) |  |  |
| has_active_session | [bool](#bool) |  |  |
| session_status | [string](#string) |  |  |
| last_location | [string](#string) |  |  |
| last_played_at | [int64](#int64) |  |  |

<a name="holomush-web-v1-ControlFrame"></a>

### ControlFrame

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| signal | [ControlSignal](#holomush-web-v1-ControlSignal) |  |  |
| message | [string](#string) |  |  |

<a name="holomush-web-v1-DisconnectRequest"></a>

### DisconnectRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  |  |

<a name="holomush-web-v1-DisconnectResponse"></a>

### DisconnectResponse

<a name="holomush-web-v1-GameEvent"></a>

### GameEvent

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| type | [string](#string) |  |  |
| category | [string](#string) |  |  |
| format | [string](#string) |  |  |
| display_target | [EventChannel](#holomush-web-v1-EventChannel) |  |  |
| timestamp | [int64](#int64) |  |  |
| actor | [string](#string) |  |  |
| text | [string](#string) |  |  |
| metadata | [google.protobuf.Struct](https://protobuf.dev/reference/protobuf/google.protobuf/#struct) |  |  |

<a name="holomush-web-v1-GetCommandHistoryRequest"></a>

### GetCommandHistoryRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  |  |

<a name="holomush-web-v1-GetCommandHistoryResponse"></a>

### GetCommandHistoryResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| commands | [string](#string) | repeated |  |

<a name="holomush-web-v1-SendCommandRequest"></a>

### SendCommandRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  |  |
| text | [string](#string) |  |  |

<a name="holomush-web-v1-SendCommandResponse"></a>

### SendCommandResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| output | [string](#string) |  |  |
| error_message | [string](#string) |  |  |

<a name="holomush-web-v1-StreamEventsRequest"></a>

### StreamEventsRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  |  |
| replay_from_cursor | [bool](#bool) |  |  |

<a name="holomush-web-v1-StreamEventsResponse"></a>

### StreamEventsResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| event | [GameEvent](#holomush-web-v1-GameEvent) |  |  |
| control | [ControlFrame](#holomush-web-v1-ControlFrame) |  |  |

<a name="holomush-web-v1-WebAuthenticatePlayerRequest"></a>

### WebAuthenticatePlayerRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| username | [string](#string) |  |  |
| password | [string](#string) |  |  |
| remember_me | [bool](#bool) |  |  |

<a name="holomush-web-v1-WebAuthenticatePlayerResponse"></a>

### WebAuthenticatePlayerResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| error_message | [string](#string) |  |  |
| characters | [CharacterSummary](#holomush-web-v1-CharacterSummary) | repeated |  |
| default_character_id | [string](#string) |  |  |

<a name="holomush-web-v1-WebCheckSessionRequest"></a>

### WebCheckSessionRequest

<a name="holomush-web-v1-WebCheckSessionResponse"></a>

### WebCheckSessionResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| player_name | [string](#string) |  |  |

<a name="holomush-web-v1-WebConfirmPasswordResetRequest"></a>

### WebConfirmPasswordResetRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| token | [string](#string) |  |  |
| new_password | [string](#string) |  |  |

<a name="holomush-web-v1-WebConfirmPasswordResetResponse"></a>

### WebConfirmPasswordResetResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| error_message | [string](#string) |  |  |

<a name="holomush-web-v1-WebContentItem"></a>

### WebContentItem

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| content_type | [string](#string) |  |  |
| body | [bytes](#bytes) |  |  |
| metadata | [WebContentItem.MetadataEntry](#holomush-web-v1-WebContentItem-MetadataEntry) | repeated |  |

<a name="holomush-web-v1-WebContentItem-MetadataEntry"></a>

### WebContentItem.MetadataEntry

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |

<a name="holomush-web-v1-WebCreateCharacterRequest"></a>

### WebCreateCharacterRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_name | [string](#string) |  |  |

<a name="holomush-web-v1-WebCreateCharacterResponse"></a>

### WebCreateCharacterResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| character_id | [string](#string) |  |  |
| character_name | [string](#string) |  |  |
| error_message | [string](#string) |  |  |

<a name="holomush-web-v1-WebCreateGuestRequest"></a>

### WebCreateGuestRequest

<a name="holomush-web-v1-WebCreateGuestResponse"></a>

### WebCreateGuestResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| error_message | [string](#string) |  |  |
| characters | [CharacterSummary](#holomush-web-v1-CharacterSummary) | repeated |  |
| default_character_id | [string](#string) |  |  |

<a name="holomush-web-v1-WebCreatePlayerRequest"></a>

### WebCreatePlayerRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| username | [string](#string) |  |  |
| password | [string](#string) |  |  |
| email | [string](#string) |  |  |

<a name="holomush-web-v1-WebCreatePlayerResponse"></a>

### WebCreatePlayerResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| characters | [CharacterSummary](#holomush-web-v1-CharacterSummary) | repeated |  |
| error_message | [string](#string) |  |  |

<a name="holomush-web-v1-WebGetContentRequest"></a>

### WebGetContentRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |

<a name="holomush-web-v1-WebGetContentResponse"></a>

### WebGetContentResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| item | [WebContentItem](#holomush-web-v1-WebContentItem) |  |  |

<a name="holomush-web-v1-WebListCharactersRequest"></a>

### WebListCharactersRequest

<a name="holomush-web-v1-WebListCharactersResponse"></a>

### WebListCharactersResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| characters | [CharacterSummary](#holomush-web-v1-CharacterSummary) | repeated |  |

<a name="holomush-web-v1-WebListContentRequest"></a>

### WebListContentRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| prefix | [string](#string) |  |  |
| limit | [int32](#int32) |  |  |
| cursor | [string](#string) |  |  |

<a name="holomush-web-v1-WebListContentResponse"></a>

### WebListContentResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| items | [WebContentItem](#holomush-web-v1-WebContentItem) | repeated |  |
| next_cursor | [string](#string) |  |  |

<a name="holomush-web-v1-WebLogoutRequest"></a>

### WebLogoutRequest

<a name="holomush-web-v1-WebLogoutResponse"></a>

### WebLogoutResponse

<a name="holomush-web-v1-WebRequestPasswordResetRequest"></a>

### WebRequestPasswordResetRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| email | [string](#string) |  |  |

<a name="holomush-web-v1-WebRequestPasswordResetResponse"></a>

### WebRequestPasswordResetResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |

<a name="holomush-web-v1-WebSelectCharacterRequest"></a>

### WebSelectCharacterRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  |  |

<a name="holomush-web-v1-WebSelectCharacterResponse"></a>

### WebSelectCharacterResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| session_id | [string](#string) |  |  |
| character_name | [string](#string) |  |  |
| reattached | [bool](#bool) |  |  |
| error_message | [string](#string) |  |  |

<a name="holomush-web-v1-ControlSignal"></a>

### ControlSignal

| Name | Number | Description |
| ---- | ------ | ----------- |
| CONTROL_SIGNAL_UNSPECIFIED | 0 |  |
| CONTROL_SIGNAL_REPLAY_COMPLETE | 1 |  |
| CONTROL_SIGNAL_STREAM_CLOSED | 2 |  |

<a name="holomush-web-v1-EventChannel"></a>

### EventChannel

| Name | Number | Description |
| ---- | ------ | ----------- |
| EVENT_CHANNEL_UNSPECIFIED | 0 |  |
| EVENT_CHANNEL_TERMINAL | 1 |  |
| EVENT_CHANNEL_STATE | 2 |  |
| EVENT_CHANNEL_BOTH | 3 |  |

<a name="holomush-web-v1-WebService"></a>

### WebService

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| SendCommand | [SendCommandRequest](#holomush-web-v1-SendCommandRequest) | [SendCommandResponse](#holomush-web-v1-SendCommandResponse) | Send a game command (say, pose, quit, etc.) |
| StreamEvents | [StreamEventsRequest](#holomush-web-v1-StreamEventsRequest) | [StreamEventsResponse](#holomush-web-v1-StreamEventsResponse) stream | Server-streaming event feed. Client receives game events (say, pose, arrive, leave) as they occur. |
| Disconnect | [DisconnectRequest](#holomush-web-v1-DisconnectRequest) | [DisconnectResponse](#holomush-web-v1-DisconnectResponse) | Disconnect ends the session and triggers cleanup. |
| GetCommandHistory | [GetCommandHistoryRequest](#holomush-web-v1-GetCommandHistoryRequest) | [GetCommandHistoryResponse](#holomush-web-v1-GetCommandHistoryResponse) | Retrieve command history for a session. |
| WebAuthenticatePlayer | [WebAuthenticatePlayerRequest](#holomush-web-v1-WebAuthenticatePlayerRequest) | [WebAuthenticatePlayerResponse](#holomush-web-v1-WebAuthenticatePlayerResponse) | Web auth RPCs. |
| WebSelectCharacter | [WebSelectCharacterRequest](#holomush-web-v1-WebSelectCharacterRequest) | [WebSelectCharacterResponse](#holomush-web-v1-WebSelectCharacterResponse) |  |
| WebCreatePlayer | [WebCreatePlayerRequest](#holomush-web-v1-WebCreatePlayerRequest) | [WebCreatePlayerResponse](#holomush-web-v1-WebCreatePlayerResponse) |  |
| WebCreateGuest | [WebCreateGuestRequest](#holomush-web-v1-WebCreateGuestRequest) | [WebCreateGuestResponse](#holomush-web-v1-WebCreateGuestResponse) | Create an ephemeral guest player and character. |
| WebCreateCharacter | [WebCreateCharacterRequest](#holomush-web-v1-WebCreateCharacterRequest) | [WebCreateCharacterResponse](#holomush-web-v1-WebCreateCharacterResponse) |  |
| WebListCharacters | [WebListCharactersRequest](#holomush-web-v1-WebListCharactersRequest) | [WebListCharactersResponse](#holomush-web-v1-WebListCharactersResponse) |  |
| WebLogout | [WebLogoutRequest](#holomush-web-v1-WebLogoutRequest) | [WebLogoutResponse](#holomush-web-v1-WebLogoutResponse) |  |
| WebRequestPasswordReset | [WebRequestPasswordResetRequest](#holomush-web-v1-WebRequestPasswordResetRequest) | [WebRequestPasswordResetResponse](#holomush-web-v1-WebRequestPasswordResetResponse) |  |
| WebConfirmPasswordReset | [WebConfirmPasswordResetRequest](#holomush-web-v1-WebConfirmPasswordResetRequest) | [WebConfirmPasswordResetResponse](#holomush-web-v1-WebConfirmPasswordResetResponse) |  |
| WebCheckSession | [WebCheckSessionRequest](#holomush-web-v1-WebCheckSessionRequest) | [WebCheckSessionResponse](#holomush-web-v1-WebCheckSessionResponse) | Validate player session from cookie. Returns player info or Unauthenticated error. |
| WebGetContent | [WebGetContentRequest](#holomush-web-v1-WebGetContentRequest) | [WebGetContentResponse](#holomush-web-v1-WebGetContentResponse) | Content store access (public, no auth required). |
| WebListContent | [WebListContentRequest](#holomush-web-v1-WebListContentRequest) | [WebListContentResponse](#holomush-web-v1-WebListContentResponse) |  |

<a name="holomush_control_v1_control-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## holomush/control/v1/control.proto

<a name="holomush-control-v1-ShutdownRequest"></a>

### ShutdownRequest

ShutdownRequest contains shutdown parameters.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| graceful | [bool](#bool) |  | If true, perform graceful shutdown allowing in-flight requests to complete. |

<a name="holomush-control-v1-ShutdownResponse"></a>

### ShutdownResponse

ShutdownResponse confirms shutdown initiation.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| message | [string](#string) |  | Human-readable status message. |

<a name="holomush-control-v1-StatusRequest"></a>

### StatusRequest

StatusRequest requests current process status.

<a name="holomush-control-v1-StatusResponse"></a>

### StatusResponse

StatusResponse contains current process status.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| running | [bool](#bool) |  | Whether the process is running. |
| pid | [int32](#int32) |  | Process ID. |
| uptime_seconds | [int64](#int64) |  | Seconds since process started. |
| component | [string](#string) |  | Component name (e.g., &#34;core&#34; or &#34;gateway&#34;). |

<a name="holomush-control-v1-ControlService"></a>

### ControlService

ControlService provides administrative operations for HoloMUSH processes.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| Shutdown | [ShutdownRequest](#holomush-control-v1-ShutdownRequest) | [ShutdownResponse](#holomush-control-v1-ShutdownResponse) | Shutdown initiates process shutdown. |
| Status | [StatusRequest](#holomush-control-v1-StatusRequest) | [StatusResponse](#holomush-control-v1-StatusResponse) | Status returns current process status. |

<a name="holomush_plugin_v1_plugin-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## holomush/plugin/v1/plugin.proto

api/proto/holomush/plugin/v1/plugin.proto

<a name="holomush-plugin-v1-CommandRequest"></a>

### CommandRequest

CommandRequest carries context for a plugin command invocation.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| command | [string](#string) |  | Parsed command name (e.g., &#34;say&#34;, &#34;dig&#34;). |
| args | [string](#string) |  | Everything after the command name. |
| raw_input | [string](#string) |  | What the player actually typed (alias support). |
| character_id | [string](#string) |  | Invoking character ULID. |
| character_name | [string](#string) |  | Character display name. |
| location_id | [string](#string) |  | Character&#39;s current location ULID. |
| session_id | [string](#string) |  | Active session ULID. |
| player_id | [string](#string) |  | Player account ULID. |

<a name="holomush-plugin-v1-CommandResponse"></a>

### CommandResponse

CommandResponse carries the result of a plugin command execution.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| status | [CommandStatus](#holomush-plugin-v1-CommandStatus) |  | Outcome category. |
| output | [string](#string) |  | Synchronous text output to the invoking player. |
| events | [EmitEvent](#holomush-plugin-v1-EmitEvent) | repeated | Events to append to the event store. |

<a name="holomush-plugin-v1-EmitEvent"></a>

### EmitEvent

EmitEvent represents an event that a plugin wants to emit.
Compatible with pkg/plugin.EmitEvent.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| stream | [string](#string) |  | Target stream for the event. |
| type | [string](#string) |  | Event type. |
| payload | [string](#string) |  | JSON-encoded payload. |

<a name="holomush-plugin-v1-Event"></a>

### Event

Event represents a game event delivered to plugins.
Compatible with pkg/plugin.Event but uses protobuf types.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | Unique event identifier (ULID string). |
| stream | [string](#string) |  | Stream the event belongs to (e.g., &#34;location:loc_abc123&#34;). |
| type | [string](#string) |  | Event type (e.g., &#34;say&#34;, &#34;pose&#34;, &#34;arrive&#34;, &#34;leave&#34;, &#34;system&#34;). |
| timestamp | [int64](#int64) |  | Timestamp in Unix milliseconds. |
| actor_kind | [string](#string) |  | Actor kind as string (e.g., &#34;character&#34;, &#34;system&#34;, &#34;plugin&#34;). Using string instead of enum for flexibility and compatibility. |
| actor_id | [string](#string) |  | Actor identifier. |
| payload | [string](#string) |  | JSON-encoded payload. |

<a name="holomush-plugin-v1-HandleCommandRequest"></a>

### HandleCommandRequest

HandleCommandRequest wraps a command for delivery to the plugin.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| command | [CommandRequest](#holomush-plugin-v1-CommandRequest) |  | The command to handle. |

<a name="holomush-plugin-v1-HandleCommandResponse"></a>

### HandleCommandResponse

HandleCommandResponse wraps the command result from the plugin.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| response | [CommandResponse](#holomush-plugin-v1-CommandResponse) |  | The command result. |

<a name="holomush-plugin-v1-HandleEventRequest"></a>

### HandleEventRequest

HandleEventRequest wraps an event for delivery to the plugin.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| event | [Event](#holomush-plugin-v1-Event) |  | The event to handle. |

<a name="holomush-plugin-v1-HandleEventResponse"></a>

### HandleEventResponse

HandleEventResponse contains any events the plugin wants to emit.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| emit_events | [EmitEvent](#holomush-plugin-v1-EmitEvent) | repeated | Events to emit in response. |

<a name="holomush-plugin-v1-InitRequest"></a>

### InitRequest

InitRequest is sent by the host after connecting to the plugin process.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| config | [ServiceConfig](#holomush-plugin-v1-ServiceConfig) |  | Service configuration for the plugin. |

<a name="holomush-plugin-v1-InitResponse"></a>

### InitResponse

InitResponse is returned by the plugin after initialization.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| provided_services | [string](#string) | repeated | gRPC service names this plugin provides on the go-plugin transport. |

<a name="holomush-plugin-v1-PluginHostServiceEmitEventRequest"></a>

### PluginHostServiceEmitEventRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| stream | [string](#string) |  |  |
| event_type | [string](#string) |  |  |
| payload | [bytes](#bytes) |  |  |

<a name="holomush-plugin-v1-PluginHostServiceEmitEventResponse"></a>

### PluginHostServiceEmitEventResponse

<a name="holomush-plugin-v1-PluginHostServiceKVDeleteRequest"></a>

### PluginHostServiceKVDeleteRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| plugin_name | [string](#string) |  |  |
| key | [string](#string) |  |  |

<a name="holomush-plugin-v1-PluginHostServiceKVDeleteResponse"></a>

### PluginHostServiceKVDeleteResponse

<a name="holomush-plugin-v1-PluginHostServiceKVGetRequest"></a>

### PluginHostServiceKVGetRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| plugin_name | [string](#string) |  |  |
| key | [string](#string) |  |  |

<a name="holomush-plugin-v1-PluginHostServiceKVGetResponse"></a>

### PluginHostServiceKVGetResponse

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| value | [string](#string) |  |  |
| found | [bool](#bool) |  |  |

<a name="holomush-plugin-v1-PluginHostServiceKVSetRequest"></a>

### PluginHostServiceKVSetRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| plugin_name | [string](#string) |  |  |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |

<a name="holomush-plugin-v1-PluginHostServiceKVSetResponse"></a>

### PluginHostServiceKVSetResponse

<a name="holomush-plugin-v1-PluginHostServiceLogRequest"></a>

### PluginHostServiceLogRequest

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| level | [string](#string) |  |  |
| message | [string](#string) |  |  |

<a name="holomush-plugin-v1-PluginHostServiceLogResponse"></a>

### PluginHostServiceLogResponse

<a name="holomush-plugin-v1-ServiceConfig"></a>

### ServiceConfig

ServiceConfig carries initialization data from the host to the plugin.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| connection_string | [string](#string) |  | PostgreSQL connection string (provided when the plugin declares storage: postgres). |
| required_services | [ServiceConfig.RequiredServicesEntry](#holomush-plugin-v1-ServiceConfig-RequiredServicesEntry) | repeated | Addresses of required services, keyed by service name (future use). |

<a name="holomush-plugin-v1-ServiceConfig-RequiredServicesEntry"></a>

### ServiceConfig.RequiredServicesEntry

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |

<a name="holomush-plugin-v1-CommandStatus"></a>

### CommandStatus

CommandStatus maps to pkg/plugin.CommandStatus values.

| Name | Number | Description |
| ---- | ------ | ----------- |
| COMMAND_STATUS_UNSPECIFIED | 0 |  |
| COMMAND_STATUS_OK | 1 |  |
| COMMAND_STATUS_ERROR | 2 |  |
| COMMAND_STATUS_FAILURE | 3 |  |
| COMMAND_STATUS_FATAL | 4 |  |

<a name="holomush-plugin-v1-PluginHostService"></a>

### PluginHostService

PluginHostService runs in the host process, allowing binary plugins
to call back for event emission, logging, and KV storage.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| EmitEvent | [PluginHostServiceEmitEventRequest](#holomush-plugin-v1-PluginHostServiceEmitEventRequest) | [PluginHostServiceEmitEventResponse](#holomush-plugin-v1-PluginHostServiceEmitEventResponse) | EmitEvent publishes an event to a stream. |
| Log | [PluginHostServiceLogRequest](#holomush-plugin-v1-PluginHostServiceLogRequest) | [PluginHostServiceLogResponse](#holomush-plugin-v1-PluginHostServiceLogResponse) | Log writes a log message through the host&#39;s logging system. |
| KVGet | [PluginHostServiceKVGetRequest](#holomush-plugin-v1-PluginHostServiceKVGetRequest) | [PluginHostServiceKVGetResponse](#holomush-plugin-v1-PluginHostServiceKVGetResponse) | KVGet retrieves a value from the plugin&#39;s key-value store. |
| KVSet | [PluginHostServiceKVSetRequest](#holomush-plugin-v1-PluginHostServiceKVSetRequest) | [PluginHostServiceKVSetResponse](#holomush-plugin-v1-PluginHostServiceKVSetResponse) | KVSet stores a value in the plugin&#39;s key-value store. |
| KVDelete | [PluginHostServiceKVDeleteRequest](#holomush-plugin-v1-PluginHostServiceKVDeleteRequest) | [PluginHostServiceKVDeleteResponse](#holomush-plugin-v1-PluginHostServiceKVDeleteResponse) | KVDelete removes a value from the plugin&#39;s key-value store. |

<a name="holomush-plugin-v1-PluginService"></a>

### PluginService

PluginService is called by the go-plugin host to send events and commands to binary plugins.
This service is implemented by the plugin (the gRPC server runs in the plugin process).

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| Init | [InitRequest](#holomush-plugin-v1-InitRequest) | [InitResponse](#holomush-plugin-v1-InitResponse) | Init is called by the host after connection, providing service configuration (DB connection string, required service addresses, etc.) and receiving the list of gRPC services the plugin provides. |
| HandleEvent | [HandleEventRequest](#holomush-plugin-v1-HandleEventRequest) | [HandleEventResponse](#holomush-plugin-v1-HandleEventResponse) | HandleEvent delivers an event to the plugin and receives any response events. |
| HandleCommand | [HandleCommandRequest](#holomush-plugin-v1-HandleCommandRequest) | [HandleCommandResponse](#holomush-plugin-v1-HandleCommandResponse) | HandleCommand delivers a command to the plugin. |

<a name="holomush_plugin_v1_hostfunc-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## holomush/plugin/v1/hostfunc.proto

api/proto/holomush/plugin/v1/hostfunc.proto

<a name="holomush-plugin-v1-CharacterInfo"></a>

### CharacterInfo

CharacterInfo contains basic character information.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | Character identifier. |
| name | [string](#string) |  | Character name. |

<a name="holomush-plugin-v1-CommandHelpInfo"></a>

### CommandHelpInfo

CommandHelpInfo contains detailed help for a command.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | Command name. |
| help | [string](#string) |  | Short help description. |
| usage | [string](#string) |  | Usage pattern. |
| help_text | [string](#string) |  | Detailed markdown help text. |
| capabilities | [string](#string) | repeated | Required capabilities for this command. |
| source | [string](#string) |  | Source plugin name or &#34;core&#34;. |

<a name="holomush-plugin-v1-CommandInfo"></a>

### CommandInfo

CommandInfo contains basic command information for listing.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | Command name. |
| help | [string](#string) |  | Short help description. |
| usage | [string](#string) |  | Usage pattern (e.g., &#34;say &lt;message&gt;&#34;). |
| source | [string](#string) |  | Source plugin name or &#34;core&#34;. |

<a name="holomush-plugin-v1-EmitEventRequest"></a>

### EmitEventRequest

EmitEventRequest wraps an event for emission by the host.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| event | [EmitEvent](#holomush-plugin-v1-EmitEvent) |  | The event to emit. |

<a name="holomush-plugin-v1-EmitEventResponse"></a>

### EmitEventResponse

EmitEventResponse indicates success or failure.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  | Whether the event was successfully emitted. |
| error | [string](#string) |  | Error message if success is false. |

<a name="holomush-plugin-v1-GetCommandHelpRequest"></a>

### GetCommandHelpRequest

GetCommandHelpRequest requests detailed help for a command.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| command_name | [string](#string) |  | Command name to get help for. |

<a name="holomush-plugin-v1-GetCommandHelpResponse"></a>

### GetCommandHelpResponse

GetCommandHelpResponse contains detailed command help.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| command | [CommandHelpInfo](#holomush-plugin-v1-CommandHelpInfo) |  | Detailed command information (nil if not found). |
| error | [string](#string) |  | Error message if query failed. |

<a name="holomush-plugin-v1-KVDeleteRequest"></a>

### KVDeleteRequest

KVDeleteRequest removes a key.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  | Key to delete. |

<a name="holomush-plugin-v1-KVDeleteResponse"></a>

### KVDeleteResponse

KVDeleteResponse indicates success or failure.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| deleted | [bool](#bool) |  | Whether the key was deleted (true even if key didn&#39;t exist). |
| error | [string](#string) |  | Error message if deletion failed. |

<a name="holomush-plugin-v1-KVGetRequest"></a>

### KVGetRequest

KVGetRequest retrieves a value by key.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  | Key to look up. |

<a name="holomush-plugin-v1-KVGetResponse"></a>

### KVGetResponse

KVGetResponse contains the retrieved value.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| value | [bytes](#bytes) |  | Value if found. |
| found | [bool](#bool) |  | Whether the key was found. |
| error | [string](#string) |  | Error message if query failed. |

<a name="holomush-plugin-v1-KVSetRequest"></a>

### KVSetRequest

KVSetRequest stores a key-value pair.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  | Key to store. |
| value | [bytes](#bytes) |  | Value to store. |

<a name="holomush-plugin-v1-KVSetResponse"></a>

### KVSetResponse

KVSetResponse indicates success or failure.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  | Whether the value was successfully stored. |
| error | [string](#string) |  | Error message if success is false. |

<a name="holomush-plugin-v1-ListCommandsRequest"></a>

### ListCommandsRequest

ListCommandsRequest requests the list of available commands.
Commands are filtered by the character&#39;s capabilities.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | Character identifier for capability filtering. |

<a name="holomush-plugin-v1-ListCommandsResponse"></a>

### ListCommandsResponse

ListCommandsResponse contains the list of available commands.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| commands | [CommandInfo](#holomush-plugin-v1-CommandInfo) | repeated | Available commands. |
| error | [string](#string) |  | Error message if query failed. |

<a name="holomush-plugin-v1-LocationInfo"></a>

### LocationInfo

LocationInfo contains basic location information.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | Location identifier. |
| name | [string](#string) |  | Location name/title. |
| description | [string](#string) |  | Location description. |

<a name="holomush-plugin-v1-LogRequest"></a>

### LogRequest

LogRequest writes a log message.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| level | [LogLevel](#holomush-plugin-v1-LogLevel) |  | Log level. |
| message | [string](#string) |  | Log message. |
| fields | [LogRequest.FieldsEntry](#holomush-plugin-v1-LogRequest-FieldsEntry) | repeated | Additional structured fields. |

<a name="holomush-plugin-v1-LogRequest-FieldsEntry"></a>

### LogRequest.FieldsEntry

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |

<a name="holomush-plugin-v1-LogResponse"></a>

### LogResponse

LogResponse is empty (logging is fire-and-forget).

<a name="holomush-plugin-v1-QueryCharacterRequest"></a>

### QueryCharacterRequest

QueryCharacterRequest requests information about a character.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character_id | [string](#string) |  | Character identifier. |

<a name="holomush-plugin-v1-QueryCharacterResponse"></a>

### QueryCharacterResponse

QueryCharacterResponse contains character information.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| character | [CharacterInfo](#holomush-plugin-v1-CharacterInfo) |  | Character information (nil if not found). |
| error | [string](#string) |  | Error message if query failed. |

<a name="holomush-plugin-v1-QueryLocationCharactersRequest"></a>

### QueryLocationCharactersRequest

QueryLocationCharactersRequest requests all characters in a location.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| location_id | [string](#string) |  | Location identifier. |

<a name="holomush-plugin-v1-QueryLocationCharactersResponse"></a>

### QueryLocationCharactersResponse

QueryLocationCharactersResponse contains the list of characters.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| characters | [CharacterInfo](#holomush-plugin-v1-CharacterInfo) | repeated | Characters in the location. |
| error | [string](#string) |  | Error message if query failed. |

<a name="holomush-plugin-v1-QueryLocationRequest"></a>

### QueryLocationRequest

QueryLocationRequest requests information about a location.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| location_id | [string](#string) |  | Location identifier. |

<a name="holomush-plugin-v1-QueryLocationResponse"></a>

### QueryLocationResponse

QueryLocationResponse contains location information.

| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| location | [LocationInfo](#holomush-plugin-v1-LocationInfo) |  | Location information (nil if not found). |
| error | [string](#string) |  | Error message if query failed. |

<a name="holomush-plugin-v1-LogLevel"></a>

### LogLevel

LogLevel specifies the severity of a log message.

| Name | Number | Description |
| ---- | ------ | ----------- |
| LOG_LEVEL_UNSPECIFIED | 0 |  |
| LOG_LEVEL_DEBUG | 1 |  |
| LOG_LEVEL_INFO | 2 |  |
| LOG_LEVEL_WARN | 3 |  |
| LOG_LEVEL_ERROR | 4 |  |

<a name="holomush-plugin-v1-HostFunctionsService"></a>

### HostFunctionsService

HostFunctionsService provides host capabilities to plugins.
This service is implemented by the host (the gRPC server runs in the host process).
Plugins call these methods to interact with the game world.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| EmitEvent | [EmitEventRequest](#holomush-plugin-v1-EmitEventRequest) | [EmitEventResponse](#holomush-plugin-v1-EmitEventResponse) | EmitEvent publishes an event to a stream. |
| QueryLocation | [QueryLocationRequest](#holomush-plugin-v1-QueryLocationRequest) | [QueryLocationResponse](#holomush-plugin-v1-QueryLocationResponse) | QueryLocation retrieves information about a location. |
| QueryCharacter | [QueryCharacterRequest](#holomush-plugin-v1-QueryCharacterRequest) | [QueryCharacterResponse](#holomush-plugin-v1-QueryCharacterResponse) | QueryCharacter retrieves information about a character. |
| QueryLocationCharacters | [QueryLocationCharactersRequest](#holomush-plugin-v1-QueryLocationCharactersRequest) | [QueryLocationCharactersResponse](#holomush-plugin-v1-QueryLocationCharactersResponse) | QueryLocationCharacters retrieves all characters in a location. |
| KVGet | [KVGetRequest](#holomush-plugin-v1-KVGetRequest) | [KVGetResponse](#holomush-plugin-v1-KVGetResponse) | KVGet retrieves a value from the plugin&#39;s key-value store. |
| KVSet | [KVSetRequest](#holomush-plugin-v1-KVSetRequest) | [KVSetResponse](#holomush-plugin-v1-KVSetResponse) | KVSet stores a value in the plugin&#39;s key-value store. |
| KVDelete | [KVDeleteRequest](#holomush-plugin-v1-KVDeleteRequest) | [KVDeleteResponse](#holomush-plugin-v1-KVDeleteResponse) | KVDelete removes a value from the plugin&#39;s key-value store. |
| Log | [LogRequest](#holomush-plugin-v1-LogRequest) | [LogResponse](#holomush-plugin-v1-LogResponse) | Log writes a log message through the host&#39;s logging system. |
| ListCommands | [ListCommandsRequest](#holomush-plugin-v1-ListCommandsRequest) | [ListCommandsResponse](#holomush-plugin-v1-ListCommandsResponse) | ListCommands returns all available commands. Requires capability: command.list |
| GetCommandHelp | [GetCommandHelpRequest](#holomush-plugin-v1-GetCommandHelpRequest) | [GetCommandHelpResponse](#holomush-plugin-v1-GetCommandHelpResponse) | GetCommandHelp returns detailed help for a specific command. Requires capability: command.help |

## Scalar Value Types

| .proto Type | Notes | C++ | Java | Python | Go | C# | PHP | Ruby |
| ----------- | ----- | --- | ---- | ------ | -- | -- | --- | ---- |
| <a name="double" /> double |  | double | double | float | float64 | double | float | Float |
| <a name="float" /> float |  | float | float | float | float32 | float | float | Float |
| <a name="int32" /> int32 | Uses variable-length encoding. Inefficient for encoding negative numbers – if your field is likely to have negative values, use sint32 instead. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="int64" /> int64 | Uses variable-length encoding. Inefficient for encoding negative numbers – if your field is likely to have negative values, use sint64 instead. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="uint32" /> uint32 | Uses variable-length encoding. | uint32 | int | int/long | uint32 | uint | integer | Bignum or Fixnum (as required) |
| <a name="uint64" /> uint64 | Uses variable-length encoding. | uint64 | long | int/long | uint64 | ulong | integer/string | Bignum or Fixnum (as required) |
| <a name="sint32" /> sint32 | Uses variable-length encoding. Signed int value. These more efficiently encode negative numbers than regular int32s. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="sint64" /> sint64 | Uses variable-length encoding. Signed int value. These more efficiently encode negative numbers than regular int64s. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="fixed32" /> fixed32 | Always four bytes. More efficient than uint32 if values are often greater than 2^28. | uint32 | int | int | uint32 | uint | integer | Bignum or Fixnum (as required) |
| <a name="fixed64" /> fixed64 | Always eight bytes. More efficient than uint64 if values are often greater than 2^56. | uint64 | long | int/long | uint64 | ulong | integer/string | Bignum |
| <a name="sfixed32" /> sfixed32 | Always four bytes. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="sfixed64" /> sfixed64 | Always eight bytes. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="bool" /> bool |  | bool | boolean | boolean | bool | bool | boolean | TrueClass/FalseClass |
| <a name="string" /> string | A string must always contain UTF-8 encoded or 7-bit ASCII text. | string | String | str/unicode | string | string | string | String (UTF-8) |
| <a name="bytes" /> bytes | May contain any arbitrary sequence of bytes. | string | ByteString | str | []byte | ByteString | string | String (ASCII-8BIT) |
