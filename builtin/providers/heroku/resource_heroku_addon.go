package heroku

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/cyberdelia/heroku-go/v3"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/hashicorp/terraform/terraform"
)

// Global lock to prevent parallelism for heroku_addon since
// the Heroku API cannot handle a single application requesting
// multiple addons simultaneously.
var addonLock sync.Mutex

func resourceHerokuAddon() *schema.Resource {
	return &schema.Resource{
		Create: resourceHerokuAddonCreate,
		Read:   resourceHerokuAddonRead,
		Update: resourceHerokuAddonUpdate,
		Delete: resourceHerokuAddonDelete,
		CreateInitialInstanceState: resourceHerokuAddonCreateInitialInstanceState,

		Schema: map[string]*schema.Schema{
			"app": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},

			"plan": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},

			"config": &schema.Schema{
				Type:     schema.TypeList,
				Optional: true,
				ForceNew: true,
				Elem: &schema.Schema{
					Type: schema.TypeMap,
				},
			},

			"provider_id": &schema.Schema{
				Type:     schema.TypeString,
				Computed: true,
			},

			"config_vars": &schema.Schema{
				Type:     schema.TypeList,
				Computed: true,
				Elem:     &schema.Schema{Type: schema.TypeMap},
			},
		},
	}
}

func resourceHerokuAddonCreate(d *schema.ResourceData, meta interface{}) error {
	addonLock.Lock()
	defer addonLock.Unlock()

	client := meta.(*heroku.Service)

	app := d.Get("app").(string)
	opts := heroku.AddonCreateOpts{Plan: d.Get("plan").(string)}

	if v := d.Get("config"); v != nil {
		config := make(map[string]string)
		for _, v := range v.([]interface{}) {
			for k, v := range v.(map[string]interface{}) {
                if k != "#" { // for some reason the number of elements pops up here
                    config[k] = v.(string)
                }
			}
		}

		opts.Config = &config
	}

	log.Printf("[DEBUG] Addon create configuration: %#v, %#v", app, opts)
	a, err := client.AddonCreate(app, opts)
	if err != nil {
		return err
	}

	d.SetId(a.ID)
	log.Printf("[INFO] Addon ID: %s", d.Id())

	return resourceHerokuAddonRead(d, meta)
}

func resourceHerokuAddonRead(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*heroku.Service)

	addon, err := resourceHerokuAddonRetrieve(
		d.Get("app").(string), d.Id(), client)
	if err != nil {
		return err
	}

	// Determine the plan. If we were configured without a specific plan,
	// then just avoid the plan altogether (accepting anything that
	// Heroku sends down).
	plan := addon.Plan.Name
	if v := d.Get("plan").(string); v != "" {
		if idx := strings.IndexRune(v, ':'); idx == -1 {
			idx = strings.IndexRune(plan, ':')
			if idx > -1 {
				plan = plan[:idx]
			}
		}
	}

	d.Set("name", addon.Name)
	d.Set("plan", plan)
	d.Set("provider_id", addon.ProviderID)

	configVarsMap := make(map[string]interface{})
	for i := range addon.ConfigVars {
		configVarsMap[addon.ConfigVars[i]] = addon.ConfigVars[i]
	}
	d.Set("config_vars", []interface{}{configVarsMap})

	return nil
}

func findAddon(addons []*heroku.Addon, plan string) *heroku.Addon {
	for i := range addons {
		addon := addons[i]
		if addon.Plan.Name == plan {
			return addon
		}
	}
	return nil
}

func resourceHerokuAddonCreateInitialInstanceState(c *terraform.ResourceConfig, state *terraform.InstanceState, meta interface{}) (*terraform.InstanceState, error) {
	client := meta.(*heroku.Service)

	app, _ := c.Get("app")
	plan, _ := c.Get("plan")
	config, _ := c.Get("config")

	addons, err := client.AddonList(app.(string), nil)
	if err != nil {
		return nil, err
	}

	addon := findAddon(addons, plan.(string))
	if addon == nil {
		return nil, fmt.Errorf("Couldn't find addon with plan %s", plan)
	}

	state.ID = addon.ID
	state.Attributes["app"] = app.(string)
	if config != nil {
		configMapsArr := config.([]map[string]interface{})
		if len(configMapsArr) > 0 {
			state.Attributes["config.#"] = "1"
			count := 0
			configMap := configMapsArr[0]
			for k, v := range configMap {
				state.Attributes["config.0."+k] = v.(string)
				count += 1
			}
			state.Attributes["config.0.#"] = fmt.Sprintf("%d", count)
		}
	}

	return state, nil
}

func resourceHerokuAddonUpdate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*heroku.Service)

	app := d.Get("app").(string)

	if d.HasChange("plan") {
		ad, err := client.AddonUpdate(
			app, d.Id(), heroku.AddonUpdateOpts{Plan: d.Get("plan").(string)})
		if err != nil {
			return err
		}

		// Store the new ID
		d.SetId(ad.ID)
	}

	return resourceHerokuAddonRead(d, meta)
}

func resourceHerokuAddonDelete(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*heroku.Service)

	log.Printf("[INFO] Deleting Addon: %s", d.Id())

	// Destroy the app
	err := client.AddonDelete(d.Get("app").(string), d.Id())
	if err != nil {
		return fmt.Errorf("Error deleting addon: %s", err)
	}

	d.SetId("")
	return nil
}

func resourceHerokuAddonRetrieve(app string, id string, client *heroku.Service) (*heroku.Addon, error) {
	addon, err := client.AddonInfo(app, id)

	if err != nil {
		return nil, fmt.Errorf("Error retrieving addon: %s", err)
	}

	return addon, nil
}

