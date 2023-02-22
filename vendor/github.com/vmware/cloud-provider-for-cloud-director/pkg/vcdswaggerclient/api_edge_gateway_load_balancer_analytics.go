/*
    Copyright 2021 VMware, Inc.
    SPDX-License-Identifier: Apache-2.0
*/


/*
 * VMware Cloud Director OpenAPI
 *
 * VMware Cloud Director OpenAPI is a new API that is defined using the OpenAPI standards.<br/> This ReSTful API borrows some elements of the legacy VMware Cloud Director API and establishes new patterns for use as described below. <h4>Authentication</h4> Authentication and Authorization schemes are the same as those for the legacy APIs. You can authenticate using the JWT token via the <code>Authorization</code> header or specifying a session using <code>x-vcloud-authorization</code> (The latter form is deprecated). <h4>Operation Patterns</h4> This API follows the following general guidelines to establish a consistent CRUD pattern: <table> <tr>   <th>Operation</th><th>Description</th><th>Response Code</th><th>Response Content</th> </tr><tr>   <td>GET /items<td>Returns a paginated list of items<td>200<td>Response will include Navigational links to the items in the list. </tr><tr>   <td>POST /items<td>Returns newly created item<td>201<td>Content-Location header links to the newly created item </tr><tr>   <td>GET /items/urn<td>Returns an individual item<td>200<td>A single item using same data type as that included in list above </tr><tr>   <td>PUT /items/urn<td>Updates an individual item<td>200<td>Updated view of the item is returned </tr><tr>   <td>DELETE /items/urn<td>Deletes the item<td>204<td>No content is returned. </tr> </table> <h5>Asynchronous operations</h5> Asynchronous operations are determined by the server. In those cases, instead of responding as described above, the server responds with an HTTP Response code 202 and an empty body. The tracking task (which is the same task as all legacy API operations use) is linked via the URI provided in the <code>Location</code> header.<br/> All API calls can choose to service a request asynchronously or synchronously as determined by the server upon interpreting the request. Operations that choose to exhibit this dual behavior will have both options documented by specifying both response code(s) below. The caller must be prepared to handle responses to such API calls by inspecting the HTTP Response code. <h5>Error Conditions</h5> <b>All</b> operations report errors using the following error reporting rules: <ul>   <li>400: Bad Request - In event of bad request due to incorrect data or other user error</li>   <li>401: Bad Request - If user is unauthenticated or their session has expired</li>   <li>403: Forbidden - If the user is not authorized or the entity does not exist</li> </ul> <h4>OpenAPI Design Concepts and Principles</h4> <ul>   <li>IDs are full Uniform Resource Names (URNs).</li>   <li>OpenAPI's <code>Content-Type</code> is always <code>application/json</code></li>   <li>REST links are in the Link header.</li>   <ul>     <li>Multiple relationships for any link are represented by multiple values in a space-separated list.</li>     <li>Links have a custom VMware Cloud Director-specific &quot;model&quot; attribute that hints at the applicable data         type for the links.</li>     <li>title + rel + model attributes evaluates to a unique link.</li>     <li>Links follow Hypermedia as the Engine of Application State (HATEOAS) principles. Links are present if         certain operations are present and permitted for the user&quot;s current role and the state of the         referred entities.</li>   </ul>   <li>APIs follow a flat structure relying on cross-referencing other entities instead of the navigational style       used by the legacy VMware Cloud Director APIs.</li>   <li>Most endpoints that return a list support filtering and sorting similar to the query service in the legacy       VMware Cloud Director APIs.</li>   <li>Accept header must be included to specify the API version for the request similar to calls to existing legacy       VMware Cloud Director APIs.</li>   <li>Each feature has a version in the path element present in its URL.<br/>       <b>Note</b> API URL's without a version in their paths must be considered experimental.</li> </ul> 
 *
 * API version: 36.0
 * Contact: https://code.vmware.com/support
 * Generated by: Swagger Codegen (https://github.com/swagger-api/swagger-codegen.git)
 */

package swagger

import (
	"context"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"github.com/antihax/optional"
)

// Linger please
var (
	_ context.Context
)

type EdgeGatewayLoadBalancerAnalyticsApiService service

/*
EdgeGatewayLoadBalancerAnalyticsApiService Retrieves analytics for a specific load balancer.
Retrieves analytics for a specific load balancer.  Metrics are specified in the filter query along with time period and series resolution.  Up to 5 metric series can be specified per report.  All reports will span the same time period.  Report filters are encapsulated in a fiql filter query parameter. Sample filter:   &lt;code&gt;filter&#x3D;(componentId&#x3D;&#x3D;urn:vcloud:virtualservice:7d38ad7f-cd93-4501-8c40-6f61650ccda0;         metric&#x3D;&#x3D;l4_server.avg_total_rtt;metric&#x3D;&#x3D;l7_server.avg_application_response_time;step&#x3D;&#x3D;500;limit&#x3D;&#x3D;100)&lt;/code&gt; Supported filters are:   &lt;ul&gt;   &lt;li&gt;componentId.  The URN of the virtual service or pool for which metrics will be gathered.  Only one should be specified.   This is required.   &lt;li&gt;metric.  One or more metrics of interest.  &lt;code&gt;filter&#x3D;(metric&#x3D;&#x3D;l4_server.avg_total_rtt;metric&#x3D;&#x3D;l7_server.avg_application_response_time)&lt;/code&gt; -   This is required.  Supported metrics can be found at the analytics/supportedMetrics endpoint.   &lt;li&gt;step.  The time resolution of the report, in seconds.   This is required.  Minimum supported resolution is 300 seconds (5 minutes).   &lt;li&gt;limit.  Optional.  The number of data points to be returned.   This is optional.  Defaults to 59 where it can&#39;t be calculated.   &lt;li&gt;startTime.  Start time of the series.   This is optional.  Must be in ISO 8601 format (i.e. 2020-07-24T00:00:00).  If not provided, start time is calculated from the step and end time.   &lt;li&gt;endTime.  End period of the series.   This is optional.  Must be in ISO 8601 format (i.e. 2020-07-24T00:00:00). Defaults to the time of latest collected data point.   &lt;/ul&gt; This feature requires additional licensing. 
 * @param ctx context.Context - for authentication, logging, cancellation, deadlines, tracing, etc. Passed from http.Request or context.Background().
 * @param optional nil or *EdgeGatewayLoadBalancerAnalyticsApiGetLoadBalancerAnalyticReportsOpts - Optional Parameters:
     * @param "Filter" (optional.String) -  Filter for a query.  FIQL format.

@return EdgeLoadBalancerAnalyticReports
*/

type EdgeGatewayLoadBalancerAnalyticsApiGetLoadBalancerAnalyticReportsOpts struct { 
	Filter optional.String
}

func (a *EdgeGatewayLoadBalancerAnalyticsApiService) GetLoadBalancerAnalyticReports(ctx context.Context, localVarOptionals *EdgeGatewayLoadBalancerAnalyticsApiGetLoadBalancerAnalyticReportsOpts) (EdgeLoadBalancerAnalyticReports, *http.Response, error) {
	var (
		localVarHttpMethod = strings.ToUpper("Get")
		localVarPostBody   interface{}
		localVarFileName   string
		localVarFileBytes  []byte
		localVarReturnValue EdgeLoadBalancerAnalyticReports
	)

	// create path and map variables
	localVarPath := a.client.cfg.BasePath + "/1.0.0/loadBalancer/analyticReports"

	localVarHeaderParams := make(map[string]string)
	localVarQueryParams := url.Values{}
	localVarFormParams := url.Values{}

	if localVarOptionals != nil && localVarOptionals.Filter.IsSet() {
		localVarQueryParams.Add("filter", parameterToString(localVarOptionals.Filter.Value(), ""))
	}
	// to determine the Content-Type header
	localVarHttpContentTypes := []string{}

	// set Content-Type header
	localVarHttpContentType := selectHeaderContentType(localVarHttpContentTypes)
	if localVarHttpContentType != "" {
		localVarHeaderParams["Content-Type"] = localVarHttpContentType
	}

	// to determine the Accept header
	localVarHttpHeaderAccepts := []string{"application/json;version=36.0"}

	// set Accept header
	localVarHttpHeaderAccept := selectHeaderAccept(localVarHttpHeaderAccepts)
	if localVarHttpHeaderAccept != "" {
		localVarHeaderParams["Accept"] = localVarHttpHeaderAccept
	}
	if ctx != nil {
		// API Key Authentication
		if auth, ok := ctx.Value(ContextAPIKey).(APIKey); ok {
			var key string
			if auth.Prefix != "" {
				key = auth.Prefix + " " + auth.Key
			} else {
				key = auth.Key
			}
			localVarHeaderParams["Authorization"] = key
			
		}
	}
	r, err := a.client.prepareRequest(ctx, localVarPath, localVarHttpMethod, localVarPostBody, localVarHeaderParams, localVarQueryParams, localVarFormParams, localVarFileName, localVarFileBytes)
	if err != nil {
		return localVarReturnValue, nil, err
	}

	localVarHttpResponse, err := a.client.callAPI(r)
	if err != nil || localVarHttpResponse == nil {
		return localVarReturnValue, localVarHttpResponse, err
	}

	localVarBody, err := ioutil.ReadAll(localVarHttpResponse.Body)
	localVarHttpResponse.Body.Close()
	if err != nil {
		return localVarReturnValue, localVarHttpResponse, err
	}

	if localVarHttpResponse.StatusCode < 300 {
		// If we succeed, return the data, otherwise pass on to decode error.
		err = a.client.decode(&localVarReturnValue, localVarBody, localVarHttpResponse.Header.Get("Content-Type"));
		return localVarReturnValue, localVarHttpResponse, err
	}

	if localVarHttpResponse.StatusCode >= 300 {
		newErr := GenericSwaggerError{
			body: localVarBody,
			error: localVarHttpResponse.Status,
		}
		
		if localVarHttpResponse.StatusCode == 200 {
			var v EdgeLoadBalancerAnalyticReports
			err = a.client.decode(&v, localVarBody, localVarHttpResponse.Header.Get("Content-Type"));
				if err != nil {
					newErr.error = err.Error()
					return localVarReturnValue, localVarHttpResponse, newErr
				}
				newErr.model = v
				return localVarReturnValue, localVarHttpResponse, newErr
		}
		
		return localVarReturnValue, localVarHttpResponse, newErr
	}

	return localVarReturnValue, localVarHttpResponse, nil
}

/*
EdgeGatewayLoadBalancerAnalyticsApiService Retrieves all the supported metrics for load balancer analytic reports.
Retrieves all the supported metrics for load balancer analytic reports.  These metrics can be used to create runtime reports of load balancer virtual services and pools. Supported filters are: &lt;ul&gt;   &lt;li&gt;componentId.  The URN of the load balancer virtual service or pool for which we want supported metrics. Only one should be specified.   This is required. &lt;/ul&gt; This feature requires additional licensing. 
 * @param ctx context.Context - for authentication, logging, cancellation, deadlines, tracing, etc. Passed from http.Request or context.Background().
 * @param optional nil or *EdgeGatewayLoadBalancerAnalyticsApiGetLoadBalancerSupportedAnalyticMetricsOpts - Optional Parameters:
     * @param "Filter" (optional.String) -  Filter for a query.  FIQL format.

@return EdgeLoadBalancerAnalyticMetrics
*/

type EdgeGatewayLoadBalancerAnalyticsApiGetLoadBalancerSupportedAnalyticMetricsOpts struct { 
	Filter optional.String
}

func (a *EdgeGatewayLoadBalancerAnalyticsApiService) GetLoadBalancerSupportedAnalyticMetrics(ctx context.Context, localVarOptionals *EdgeGatewayLoadBalancerAnalyticsApiGetLoadBalancerSupportedAnalyticMetricsOpts) (EdgeLoadBalancerAnalyticMetrics, *http.Response, error) {
	var (
		localVarHttpMethod = strings.ToUpper("Get")
		localVarPostBody   interface{}
		localVarFileName   string
		localVarFileBytes  []byte
		localVarReturnValue EdgeLoadBalancerAnalyticMetrics
	)

	// create path and map variables
	localVarPath := a.client.cfg.BasePath + "/1.0.0/loadBalancer/analyticReports/supportedMetrics"

	localVarHeaderParams := make(map[string]string)
	localVarQueryParams := url.Values{}
	localVarFormParams := url.Values{}

	if localVarOptionals != nil && localVarOptionals.Filter.IsSet() {
		localVarQueryParams.Add("filter", parameterToString(localVarOptionals.Filter.Value(), ""))
	}
	// to determine the Content-Type header
	localVarHttpContentTypes := []string{}

	// set Content-Type header
	localVarHttpContentType := selectHeaderContentType(localVarHttpContentTypes)
	if localVarHttpContentType != "" {
		localVarHeaderParams["Content-Type"] = localVarHttpContentType
	}

	// to determine the Accept header
	localVarHttpHeaderAccepts := []string{"application/json;version=36.0"}

	// set Accept header
	localVarHttpHeaderAccept := selectHeaderAccept(localVarHttpHeaderAccepts)
	if localVarHttpHeaderAccept != "" {
		localVarHeaderParams["Accept"] = localVarHttpHeaderAccept
	}
	if ctx != nil {
		// API Key Authentication
		if auth, ok := ctx.Value(ContextAPIKey).(APIKey); ok {
			var key string
			if auth.Prefix != "" {
				key = auth.Prefix + " " + auth.Key
			} else {
				key = auth.Key
			}
			localVarHeaderParams["Authorization"] = key
			
		}
	}
	r, err := a.client.prepareRequest(ctx, localVarPath, localVarHttpMethod, localVarPostBody, localVarHeaderParams, localVarQueryParams, localVarFormParams, localVarFileName, localVarFileBytes)
	if err != nil {
		return localVarReturnValue, nil, err
	}

	localVarHttpResponse, err := a.client.callAPI(r)
	if err != nil || localVarHttpResponse == nil {
		return localVarReturnValue, localVarHttpResponse, err
	}

	localVarBody, err := ioutil.ReadAll(localVarHttpResponse.Body)
	localVarHttpResponse.Body.Close()
	if err != nil {
		return localVarReturnValue, localVarHttpResponse, err
	}

	if localVarHttpResponse.StatusCode < 300 {
		// If we succeed, return the data, otherwise pass on to decode error.
		err = a.client.decode(&localVarReturnValue, localVarBody, localVarHttpResponse.Header.Get("Content-Type"));
		return localVarReturnValue, localVarHttpResponse, err
	}

	if localVarHttpResponse.StatusCode >= 300 {
		newErr := GenericSwaggerError{
			body: localVarBody,
			error: localVarHttpResponse.Status,
		}
		
		if localVarHttpResponse.StatusCode == 200 {
			var v EdgeLoadBalancerAnalyticMetrics
			err = a.client.decode(&v, localVarBody, localVarHttpResponse.Header.Get("Content-Type"));
				if err != nil {
					newErr.error = err.Error()
					return localVarReturnValue, localVarHttpResponse, newErr
				}
				newErr.model = v
				return localVarReturnValue, localVarHttpResponse, newErr
		}
		
		return localVarReturnValue, localVarHttpResponse, newErr
	}

	return localVarReturnValue, localVarHttpResponse, nil
}

