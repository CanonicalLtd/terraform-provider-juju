package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	"github.com/juju/terraform-provider-juju/internal/juju"
)

func resourceApplication() *schema.Resource {
	return &schema.Resource{
		Description: "A resource that represents a Juju application deployment.",

		CreateContext: resourceApplicationCreate,
		ReadContext:   resourceApplicationRead,
		UpdateContext: resourceApplicationUpdate,
		DeleteContext: resourceApplicationDelete,

		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			"name": {
				Description: "A custom name for the application deployment. If empty, uses the charm's name.",
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
			},
			"model": {
				Description: "The name of the model where the application is to be deployed.",
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
			},
			"charm": {
				Description: "The name of the charm to be installed from Charmhub.",
				Type:        schema.TypeList,
				Required:    true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name": {
							Description: "The name of the charm",
							Type:        schema.TypeString,
							Required:    true,
							ForceNew:    true,
						},
						"channel": {
							Description: "The channel to use when deploying a charm. Specified as <track>/<risk>/<branch>.",
							Type:        schema.TypeString,
							Default:     "latest/stable",
							Optional:    true,
						},
						"revision": {
							Description: "The revision of the charm to deploy.",
							Type:        schema.TypeInt,
							Optional:    true,
							Computed:    true,
						},
						"series": {
							Description: "The series on which to deploy.",
							Type:        schema.TypeString,
							Optional:    true,
							Computed:    true,
						},
					},
				},
			},
			"units": {
				Description: "The number of application units to deploy for the charm.",
				Type:        schema.TypeInt,
				Optional:    true,
				Default:     1,
			},
			"config": {
				Description: "Application specific configuration.",
				Type:        schema.TypeMap,
				Optional:    true,
				DefaultFunc: func() (interface{}, error) {
					return make(map[string]interface{}), nil
				},
			},
			"trust": {
				Description: "Set the trust for the application.",
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
			},
			"expose": {
				Description: "Makes an application publicly available over the network",
				Type:        schema.TypeList,
				Optional:    true,
				Default:     nil,
				MaxItems:    1,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"endpoints": {
							Description: "Expose only the ports that charms have opened for this comma-delimited list of endpoints",
							Type:        schema.TypeString,
							Default:     "",
							Optional:    true,
						},
						"spaces": {
							Description: "A comma-delimited list of spaces that should be able to access the application ports once exposed.",
							Type:        schema.TypeString,
							Default:     "",
							Optional:    true,
						},
						"cidrs": {
							Description: "A comma-delimited list of CIDRs that should be able to access the application ports once exposed.",
							Type:        schema.TypeString,
							Default:     "",
							Optional:    true,
						},
					},
				},
			},
		},
	}
}

func resourceApplicationCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*juju.Client)

	modelName := d.Get("model").(string)
	modelUUID, err := client.Models.ResolveModelUUID(modelName)
	if err != nil {
		return diag.FromErr(err)
	}

	name := d.Get("name").(string)
	charm := d.Get("charm").([]interface{})[0].(map[string]interface{})
	charmName := charm["name"].(string)
	channel := charm["channel"].(string)
	series := charm["series"].(string)
	units := d.Get("units").(int)
	trust := d.Get("trust").(bool)
	// populate the config parameter
	configField := d.Get("config").(map[string]interface{})
	config := make(map[string]string)
	for k, v := range configField {
		config[k] = v.(string)
	}
	// if expose is nil, it was not defined
	var expose map[string]interface{} = nil
	exposeField, exposeWasSet := d.GetOk("expose")
	if exposeWasSet {
		// this was set, by default get no fields there
		expose = make(map[string]interface{}, 0)
		aux := exposeField.([]interface{})[0]
		if aux != nil {
			expose = aux.(map[string]interface{})
		}
	}

	revision := charm["revision"].(int)
	if _, exist := d.GetOk("charm.0.revision"); !exist {
		revision = -1
	}

	response, err := client.Applications.CreateApplication(&juju.CreateApplicationInput{
		ApplicationName: name,
		ModelUUID:       modelUUID,
		CharmName:       charmName,
		CharmChannel:    channel,
		CharmRevision:   revision,
		CharmSeries:     series,
		Units:           units,
		Config:          config,
		Trust:           trust,
		Expose:          expose,
	})

	if err != nil {
		return diag.FromErr(err)
	}

	// These values can be computed, and so set from the response.
	if err = d.Set("name", response.AppName); err != nil {
		return diag.FromErr(err)
	}

	charm["revision"] = response.Revision
	charm["series"] = response.Series
	if err = d.Set("charm", []map[string]interface{}{charm}); err != nil {
		return diag.FromErr(err)
	}

	id := fmt.Sprintf("%s:%s", modelName, response.AppName)
	d.SetId(id)

	return nil
}

func resourceApplicationRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*juju.Client)
	id := strings.Split(d.Id(), ":")
	//If importing with an incorrect ID we need to catch and provide a user-friendly error
	if len(id) != 2 {
		return diag.Errorf("unable to parse model and application name from provided ID")
	}
	modelName, appName := id[0], id[1]
	modelUUID, err := client.Models.ResolveModelUUID(modelName)
	if err != nil {
		return diag.FromErr(err)
	}

	response, err := client.Applications.ReadApplication(&juju.ReadApplicationInput{
		ModelUUID: modelUUID,
		AppName:   appName,
	})
	if err != nil {
		return diag.FromErr(err)
	}

	if response == nil {
		return nil
	}

	var charmList map[string]interface{}
	_, exists := d.GetOk("charm")
	if exists {
		charmList = d.Get("charm").([]interface{})[0].(map[string]interface{})
		charmList["name"] = response.Name
		charmList["channel"] = response.Channel
		charmList["revision"] = response.Revision
		charmList["series"] = response.Series
	} else {
		charmList = map[string]interface{}{
			"name":     response.Name,
			"channel":  response.Channel,
			"revision": response.Revision,
			"series":   response.Series,
		}
	}
	if err = d.Set("charm", []map[string]interface{}{charmList}); err != nil {
		return diag.FromErr(err)
	}

	if err = d.Set("model", modelName); err != nil {
		return diag.FromErr(err)
	}
	if err = d.Set("name", appName); err != nil {
		return diag.FromErr(err)
	}
	if err = d.Set("units", response.Units); err != nil {
		return diag.FromErr(err)
	}

	if err = d.Set("trust", response.Trust); err != nil {
		return diag.FromErr(err)
	}

	var exposeValue []map[string]interface{} = nil
	if response.Expose != nil {
		exposeValue = []map[string]interface{}{response.Expose}
	}
	if err = d.Set("expose", exposeValue); err != nil {
		return diag.FromErr(err)
	}

	// config will contain a long map with many fields this plan
	// may not be aware of. We run a diff and only focus on those
	// entries we know from the previous state. If they were removed
	// from the previous state that has been modified.
	previousConfig := d.Get("config").(map[string]interface{})
	newConfig := make(map[string]interface{}, 0)
	for k := range previousConfig {
		newConfig[k] = response.Config[k]
	}
	if err = d.Set("config", newConfig); err != nil {
		return diag.FromErr(err)
	}

	return nil
}

func resourceApplicationUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*juju.Client)

	appName := d.Get("name").(string)
	modelName := d.Get("model").(string)
	modelInfo, err := client.Models.GetModelByName(modelName)
	if err != nil {
		return diag.FromErr(err)
	}
	updateApplicationInput := juju.UpdateApplicationInput{
		ModelUUID: modelInfo.UUID,
		ModelType: modelInfo.Type,
		AppName:   appName,
	}

	if d.HasChange("units") {
		units := d.Get("units").(int)
		updateApplicationInput.Units = &units
	}

	if d.HasChange("trust") {
		trust := d.Get("trust").(bool)
		updateApplicationInput.Trust = &trust
	}

	if d.HasChange("expose") {
		oldExpose, newExpose := d.GetChange("expose")
		_, exposeWasSet := d.GetOk("expose")

		expose, unexpose := computeExposeDeltas(oldExpose, newExpose, exposeWasSet)

		updateApplicationInput.Expose = expose
		updateApplicationInput.Unexpose = unexpose
	}

	if d.HasChange("charm.0.revision") {
		revision := d.Get("charm.0.revision").(int)
		updateApplicationInput.Revision = &revision
	}

	if d.HasChange("config") {
		config := d.Get("config").(map[string]interface{})
		updateApplicationInput.Config = make(map[string]string, len(config))
		for k, v := range config {
			updateApplicationInput.Config[k] = v.(string)
		}
	}

	err = client.Applications.UpdateApplication(&updateApplicationInput)
	if err != nil {
		return diag.FromErr(err)
	}

	return nil
}

// computeExposeDeltas computes the differences between the previously
// stored expose value and the current one. The valueSet argument is used
// to indicate whether the value was already set or not in the latest
// read of the plan.
func computeExposeDeltas(oldExpose interface{}, newExpose interface{}, valueSet bool) (map[string]interface{}, []string) {
	var old map[string]interface{} = nil
	var new map[string]interface{} = nil

	if oldExpose != nil {
		aux := oldExpose.([]interface{})
		if len(aux) != 0 && aux[0] != nil {
			old = aux[0].(map[string]interface{})
		}
	}
	if newExpose != nil {
		aux := newExpose.([]interface{})
		if len(aux) != 0 && aux[0] != nil {
			new = aux[0].(map[string]interface{})
		}
	}
	if new == nil && valueSet {
		new = make(map[string]interface{})
	}

	toExpose := make(map[string]interface{})
	toUnexpose := make([]string, 0)
	// if new is nil we unexpose everything
	if new == nil {
		// set all the endpoints to be unexposed
		toUnexpose = append(toUnexpose, old["endpoints"].(string))
		return nil, toUnexpose
	}

	if old != nil {
		old = make(map[string]interface{})
	}

	// if we have new endpoints we have to expose them
	for endpoint, v := range new {
		_, found := old[endpoint]
		if found {
			// this was already set
			// If it is different, unexpose and then expose
			if v != old[endpoint] {
				toUnexpose = append(toUnexpose, endpoint)
				toExpose[endpoint] = v
			}
		} else {
			// this was not set, expose it
			toExpose[endpoint] = v
		}
	}
	return toExpose, toUnexpose
}

// Juju refers to deletion as "destroy" so we call the Destroy function of our client here rather than delete
// This function remains named Delete for parity across the provider and to stick within terraform naming conventions
func resourceApplicationDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*juju.Client)

	modelName := d.Get("model").(string)
	modelUUID, err := client.Models.ResolveModelUUID(modelName)
	if err != nil {
		return diag.FromErr(err)
	}

	var diags diag.Diagnostics

	err = client.Applications.DestroyApplication(&juju.DestroyApplicationInput{
		ApplicationName: d.Get("name").(string),
		ModelUUID:       modelUUID,
	})
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId("")
	return diags
}
