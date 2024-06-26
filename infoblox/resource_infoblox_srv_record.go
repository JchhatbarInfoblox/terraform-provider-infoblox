package infoblox

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	ibclient "github.com/infobloxopen/infoblox-go-client/v2"
)

func resourceSRVRecord() *schema.Resource {
	return &schema.Resource{
		Create: resourceSRVRecordCreate,
		Read:   resourceSRVRecordGet,
		Update: resourceSRVRecordUpdate,
		Delete: resourceSRVRecordDelete,

		Importer: &schema.ResourceImporter{
			State: resourceSRVRecordImport,
		},
		CustomizeDiff: func(context context.Context, d *schema.ResourceDiff, meta interface{}) error {
			if internalID := d.Get("internal_id"); internalID == "" || internalID == nil {
				err := d.SetNewComputed("internal_id")
				if err != nil {
					return err
				}
			}
			return nil
		},

		Schema: map[string]*schema.Schema{
			"dns_view": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     defaultDNSView,
				Description: "DNS view which the zone does exist within",
			},
			"name": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "Combination of service's name, protocol's name and zone's name",
			},
			"priority": {
				Type:        schema.TypeInt,
				Required:    true,
				Description: "Configures the priority (0..65535) for this SRV-record.",
			},
			"weight": {
				Type:        schema.TypeInt,
				Required:    true,
				Description: "Configures weight of the SRV-record, valid values are 0..65535.",
			},
			"port": {
				Type:        schema.TypeInt,
				Required:    true,
				Description: "Configures port number (0..65535) for this SRV-record.",
			},
			"target": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "Provides service for domain name in the SRV-record.",
			},
			"ttl": {
				Type:        schema.TypeInt,
				Optional:    true,
				Default:     ttlUndef,
				Description: "TTL value for the SRV-record.",
			},
			"comment": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "",
				Description: "Description of the SRV-record",
			},
			"ext_attrs": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "",
				Description: "Extensible attributes of the SRV-record to be added/updated, as a map in JSON format.",
			},
			"internal_id": {
				Type:     schema.TypeString,
				Computed: true,
				Description: "Internal ID of an object at NIOS side," +
					" used by Infoblox Terraform plugin to search for a NIOS's object" +
					" which corresponds to the Terraform resource.",
			},
			"ref": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "NIOS object's reference, not to be set by a user.",
			},
		},
	}
}

func resourceSRVRecordCreate(d *schema.ResourceData, m interface{}) error {

	if intId := d.Get("internal_id"); intId.(string) != "" {
		return fmt.Errorf("the value of 'internal_id' field must not be set manually")
	}

	dnsView := d.Get("dns_view").(string)

	// the next group of parameters will be validated inside ibclient.CreateSRVRecord()
	name := d.Get("name").(string)
	priority := d.Get("priority").(int)
	weight := d.Get("weight").(int)
	port := d.Get("port").(int)
	target := d.Get("target").(string)

	var ttl uint32
	useTtl := false
	tempVal := d.Get("ttl")
	tempTTL := tempVal.(int)
	if tempTTL >= 0 {
		useTtl = true
		ttl = uint32(tempTTL)
	} else if tempTTL != ttlUndef {
		return fmt.Errorf("TTL value must be 0 or higher")
	}

	comment := d.Get("comment").(string)

	extAttrJSON := d.Get("ext_attrs").(string)
	extAttrs, err := terraformDeserializeEAs(extAttrJSON)
	if err != nil {
		return err
	}

	// Generate internal ID and add it to the extensible attributes
	internalId := generateInternalId()
	extAttrs[eaNameForInternalId] = internalId.String()

	var tenantID string
	tempVal, found := extAttrs[eaNameForTenantId]
	if found {
		tenantID = tempVal.(string)
	}
	connector := m.(ibclient.IBConnector)
	objMgr := ibclient.NewObjectManager(connector, "Terraform", tenantID)

	newRecord, err := objMgr.CreateSRVRecord(
		dnsView, name, uint32(priority), uint32(weight), uint32(port), target, ttl, useTtl, comment, extAttrs)

	if err != nil {
		return fmt.Errorf("error creating SRV-record: %s", err.Error())
	}

	d.SetId(newRecord.Ref)

	if err = d.Set("ref", newRecord.Ref); err != nil {
		return err
	}
	if err = d.Set("internal_id", internalId.String()); err != nil {
		return err
	}

	return nil
}

func resourceSRVRecordGet(d *schema.ResourceData, m interface{}) error {
	var ttl int
	extAttrJSON := d.Get("ext_attrs").(string)
	extAttrs, err := terraformDeserializeEAs(extAttrJSON)
	if err != nil {
		return err
	}

	rec, err := searchObjectByRefOrInternalId("SRV", d, m)
	if err != nil {
		if _, ok := err.(*ibclient.NotFoundError); !ok {
			return ibclient.NewNotFoundError(fmt.Sprintf(
				"cannot find appropriate object on NIOS side for resource with ID '%s': %s;", d.Id(), err))
		} else {
			d.SetId("")
			return nil
		}
	}

	// Assertion of object type and error handling
	var obj *ibclient.RecordSRV
	recJson, _ := json.Marshal(rec)
	err = json.Unmarshal(recJson, &obj)

	if err != nil && obj.Ref != "" {
		return fmt.Errorf("failed getting SRV-Record: %s", err.Error())
	}

	if obj.Ttl != nil {
		ttl = int(*obj.Ttl)
	}

	if !*obj.UseTtl {
		ttl = ttlUndef
	}
	if err = d.Set("ttl", ttl); err != nil {
		return err
	}

	delete(obj.Ea, eaNameForInternalId)
	omittedEAs := omitEAs(obj.Ea, extAttrs)

	if omittedEAs != nil && len(omittedEAs) > 0 {
		eaJSON, err := terraformSerializeEAs(omittedEAs)
		if err != nil {
			return err
		}
		if err = d.Set("ext_attrs", eaJSON); err != nil {
			return err
		}
	}

	if err = d.Set("comment", obj.Comment); err != nil {
		return err
	}
	if err = d.Set("dns_view", obj.View); err != nil {
		return err
	}
	if err = d.Set("name", obj.Name); err != nil {
		return err
	}
	if err = d.Set("priority", obj.Priority); err != nil {
		return err
	}
	if err = d.Set("ref", obj.Ref); err != nil {
		return err
	}
	if err = d.Set("weight", obj.Weight); err != nil {
		return err
	}
	if err = d.Set("port", obj.Port); err != nil {
		return err
	}
	if err = d.Set("target", obj.Target); err != nil {
		return err
	}
	d.SetId(obj.Ref)

	return nil
}

func resourceSRVRecordUpdate(d *schema.ResourceData, m interface{}) error {
	var updateSuccessful bool
	defer func() {
		// Reverting the state back, in case of a failure,
		// otherwise Terraform will keep the values, which leaded to the failure,
		// in the state file.

		if !updateSuccessful {
			prevDNSView, _ := d.GetChange("dns_view")
			prevName, _ := d.GetChange("name")
			prevPriority, _ := d.GetChange("priority")
			prevWeight, _ := d.GetChange("weight")
			prevPort, _ := d.GetChange("port")
			prevTarget, _ := d.GetChange("target")
			prevTTL, _ := d.GetChange("ttl")
			prevComment, _ := d.GetChange("comment")
			prevEa, _ := d.GetChange("ext_attrs")

			_ = d.Set("dns_view", prevDNSView.(string))
			_ = d.Set("name", prevName.(string))
			_ = d.Set("priority", prevPriority.(int))
			_ = d.Set("weight", prevWeight.(int))
			_ = d.Set("port", prevPort.(int))
			_ = d.Set("target", prevTarget.(string))
			_ = d.Set("ttl", prevTTL.(int))
			_ = d.Set("comment", prevComment.(string))
			_ = d.Set("ext_attrs", prevEa.(string))
		}
	}()

	if d.HasChange("internal_id") {
		return fmt.Errorf("changing the value of 'internal_id' field is not allowed")
	}

	if d.HasChange("dns_view") {
		return fmt.Errorf("changing the value of 'dns_view' field is not allowed")
	}

	// the next group of parameters will be validated inside ibclient.UpdateSRVRecord()
	name := d.Get("name").(string)
	priority := d.Get("priority").(int)
	weight := d.Get("weight").(int)
	port := d.Get("port").(int)
	target := d.Get("target").(string)

	var ttl uint32
	useTtl := false
	tempVal := d.Get("ttl")
	tempTTL := tempVal.(int)
	if tempTTL >= 0 {
		useTtl = true
		ttl = uint32(tempTTL)
	} else if tempTTL != ttlUndef {
		return fmt.Errorf("TTL value must be 0 or higher")
	}

	comment := d.Get("comment").(string)

	oldExtAttrsJSON, newExtAttrsJSON := d.GetChange("ext_attrs")

	newExtAttrs, err := terraformDeserializeEAs(newExtAttrsJSON.(string))
	if err != nil {
		return err
	}

	oldExtAttrs, err := terraformDeserializeEAs(oldExtAttrsJSON.(string))
	if err != nil {
		return err
	}

	var tenantID string
	tempVal, found := newExtAttrs[eaNameForTenantId]
	if found {
		tenantID = tempVal.(string)
	}
	connector := m.(ibclient.IBConnector)
	objMgr := ibclient.NewObjectManager(connector, "Terraform", tenantID)

	srvrec, err := objMgr.GetSRVRecordByRef(d.Id())
	if err != nil {
		return fmt.Errorf("failed to read SRV Record for update operation: %w", err)
	}

	internalId := d.Get("internal_id").(string)

	if internalId == "" {
		internalId = generateInternalId().String()
	}

	newInternalId := newInternalResourceIdFromString(internalId)
	newExtAttrs[eaNameForInternalId] = newInternalId.String()

	newExtAttrs, err = mergeEAs(srvrec.Ea, newExtAttrs, oldExtAttrs, connector)
	if err != nil {
		return err
	}

	rec, err := objMgr.UpdateSRVRecord(
		d.Id(), name, uint32(priority), uint32(weight), uint32(port), target, ttl, useTtl, comment, newExtAttrs)
	if err != nil {
		return fmt.Errorf("error updating SRV-Record: %s", err.Error())
	}
	updateSuccessful = true
	d.SetId(rec.Ref)
	if err = d.Set("ref", rec.Ref); err != nil {
		return err
	}
	if err = d.Set("internal_id", newInternalId.String()); err != nil {
		return err
	}

	return nil
}

func resourceSRVRecordDelete(d *schema.ResourceData, m interface{}) error {
	extAttrJSON := d.Get("ext_attrs").(string)
	extAttrs, err := terraformDeserializeEAs(extAttrJSON)
	if err != nil {
		return err
	}

	var tenantID string
	tempVal, found := extAttrs[eaNameForTenantId]
	if found {
		tenantID = tempVal.(string)
	}

	connector := m.(ibclient.IBConnector)
	objMgr := ibclient.NewObjectManager(connector, "Terraform", tenantID)
	srvrec, err := searchObjectByRefOrInternalId("SRV", d, m)
	if err != nil {
		if _, ok := err.(*ibclient.NotFoundError); !ok {
			return ibclient.NewNotFoundError(fmt.Sprintf(
				"cannot find appropriate object on NIOS side for resource with ID '%s': %s;", d.Id(), err))
		} else {
			d.SetId("")
			return nil
		}
	}

	// Assertion of object type and error handling
	var obj ibclient.RecordSRV
	recJson, _ := json.Marshal(srvrec)
	err = json.Unmarshal(recJson, &obj)

	if err != nil {
		return fmt.Errorf("failed getting SRV-Record: %s", err.Error())
	}

	_, err = objMgr.DeleteSRVRecord(obj.Ref)
	if err != nil {
		return fmt.Errorf("deletion of MX-Record failed: %s", err.Error())
	}
	d.SetId("")

	return nil

}

func resourceSRVRecordImport(d *schema.ResourceData, m interface{}) ([]*schema.ResourceData, error) {
	var ttl int
	extAttrJSON := d.Get("ext_attrs").(string)
	extAttrs, err := terraformDeserializeEAs(extAttrJSON)
	if err != nil {
		return nil, err
	}
	var tenantID string
	tempVal, found := extAttrs[eaNameForTenantId]
	if found {
		tenantID = tempVal.(string)
	}

	connector := m.(ibclient.IBConnector)
	objMgr := ibclient.NewObjectManager(connector, "Terraform", tenantID)

	obj, err := objMgr.GetSRVRecordByRef(d.Id())
	if err != nil {
		return nil, fmt.Errorf("failed getting SRV-Record: %s", err.Error())
	}

	if obj.Ttl != nil {
		ttl = int(*obj.Ttl)
	}

	if !*obj.UseTtl {
		ttl = ttlUndef
	}
	if err = d.Set("ttl", ttl); err != nil {
		return nil, err
	}

	// Set ref
	if err = d.Set("ref", obj.Ref); err != nil {
		return nil, err
	}

	if obj.Ea != nil && len(obj.Ea) > 0 {
		eaJSON, err := terraformSerializeEAs(obj.Ea)
		if err != nil {
			return nil, err
		}
		if err = d.Set("ext_attrs", eaJSON); err != nil {
			return nil, err
		}
	}

	if err = d.Set("comment", obj.Comment); err != nil {
		return nil, err
	}
	if err = d.Set("dns_view", obj.View); err != nil {
		return nil, err
	}
	if err = d.Set("name", obj.Name); err != nil {
		return nil, err
	}
	if err = d.Set("priority", obj.Priority); err != nil {
		return nil, err
	}
	if err = d.Set("weight", obj.Weight); err != nil {
		return nil, err
	}
	if err = d.Set("port", obj.Port); err != nil {
		return nil, err
	}
	if err = d.Set("target", obj.Target); err != nil {
		return nil, err
	}
	d.SetId(obj.Ref)

	err = resourceSRVRecordUpdate(d, m)
	if err != nil {
		return nil, err
	}

	return []*schema.ResourceData{d}, nil
}
