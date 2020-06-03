package k8spspallowedusers

# fails on mutant `accept_users("RunAsAny", provided_user) {false}`
test_one_container_run_as_any {
  results := data.k8spspallowedusers.violation with input as
  {
  	"review": {
  	  "kind": {
  	    "kind": "Pod"
  	  },
  		"object": {
  			"spec": {
  				"containers": [{
  					"name": "container1",
  					"securityContext": {
  						"runAsUser": 12
  					}
  				}]
  			}
  		}
  	},
  	"parameters": {
  		"runAsUser": {
  			"rule": "RunAsAny"
  		}
  	}
  }
  count(results) == 0
}


# fails on mutant `accept_users("RunAsAny", provided_user) {false}`
test_one_container_run_as_any_root_user {
  results := violation with input as
  {
  	"review": {
  	  "kind": {
  	    "kind": "Pod"
  	  },
  		"object": {
  			"spec": {
  				"containers": [{
  					"name": "container1",
  					"securityContext": {
  						"runAsUser": 0
  					}
  				}]
  			}
  		}
  	},
  	"parameters": {
  		"runAsUser": {
  			"rule": "RunAsAny"
  		}
  	}
  }
  count(results) == 0
}

test_one_container_run_as_non_root_user_is_not_root {
  results := violation with input as
  {
  	"review": {
  	  "kind": {
  	    "kind": "Pod"
  	  },
  		"object": {
  			"spec": {
  				"containers": [{
  					"name": "container1",
  					"securityContext": {
  						"runAsUser": 1
  					}
  				}]
  			}
  		}
  	},
  	"parameters": {
  		"runAsUser": {
  			"rule": "MustRunAsNonRoot"
  		}
  	}
  }
  count(results) == 0
}


test_one_container_run_as_non_root_user_is_root {
  results := violation with input as
  {
  	"review": {
  	  "kind": {
  	    "kind": "Pod"
  	  },
  		"object": {
  			"spec": {
  				"containers": [{
  					"name": "container1",
  					"securityContext": {
  						"runAsUser": 0
  					}
  				}]
  			}
  		}
  	},
  	"parameters": {
  		"runAsUser": {
  			"rule": "MustRunAsNonRoot"
  		}
  	}
  }
  count(results) > 0
}

test_one_container_run_in_range_user_in_range {
  results := violation with input as
  {
  	"review": {
  	  "kind": {
  	    "kind": "Pod"
  	  },
  		"object": {
  			"spec": {
  				"containers": [{
  					"name": "container1",
  					"securityContext": {
  						"runAsUser": 150
  					}
  				}]
  			}
  		}
  	},
  	"parameters": {
  		"runAsUser": {
  			"rule": "MustRunAs",
  			"ranges": [{
  				"min": 100,
  				"max": 200
  			}]
  		}
  	}
  }
  count(results) == 0
}

test_one_container_run_in_range_user_out_of_range {
  results := violation with input as
  {
  	"review": {
  	  "kind": {
  	    "kind": "Pod"
  	  },
  		"object": {
  			"spec": {
  				"containers": [{
  					"name": "container1",
  					"securityContext": {
  						"runAsUser": 10
  					}
  				}]
  			}
  		}
  	},
  	"parameters": {
  		"runAsUser": {
  			"rule": "MustRunAs",
  			"ranges": [{
  				"min": 100,
  				"max": 200
  			}]
  		}
  	}
  }
  count(results) > 0
}

test_one_container_run_in_range_user_lower_edge_of_range {
  results := violation with input as
  {
  	"review": {
  	  "kind": {
  	    "kind": "Pod"
  	  },
  		"object": {
  			"spec": {
  				"containers": [{
  					"name": "container1",
  					"securityContext": {
  						"runAsUser": 100
  					}
  				}]
  			}
  		}
  	},
  	"parameters": {
  		"runAsUser": {
  			"rule": "MustRunAs",
  			"ranges": [{
  				"min": 100,
  				"max": 200
  			}]
  		}
  	}
  }
  count(results) == 0
}

test_one_container_run_in_range_user_upper_edge_of_range {
  results := violation with input as
  {
  	"review": {
  	  "kind": {
  	    "kind": "Pod"
  	  },
  		"object": {
  			"spec": {
  				"containers": [{
  					"name": "container1",
  					"securityContext": {
  						"runAsUser": 200
  					}
  				}]
  			}
  		}
  	},
  	"parameters": {
  		"runAsUser": {
  			"rule": "MustRunAs",
  			"ranges": [{
  				"min": 100,
  				"max": 200
  			}]
  		}
  	}
  }
  count(results) == 0
}

test_one_container_run_in_range_user_between_ranges {
  results := violation with input as
  {
  	"review": {
  	  "kind": {
  	    "kind": "Pod"
  	  },
  		"object": {
  			"spec": {
  				"containers": [{
  					"name": "container1",
  					"securityContext": {
  						"runAsUser": 200
  					}
  				}]
  			}
  		}
  	},
  	"parameters": {
  		"runAsUser": {
  			"rule": "MustRunAs",
  			"ranges": [{
  				"min": 100,
  				"max": 100
  			}, {
  				"min": 250,
  				"max": 250
  			}]
  		}
  	}
  }
  count(results) > 0
}

test_two_containers_run_as_any {
  results := violation with input as
  {
  	"review": {
  	  "kind": {
  	    "kind": "Pod"
  	  },
  		"object": {
  			"spec": {
  				"containers": [{
  						"name": "container1",
  						"securityContext": {
  							"runAsUser": 1
  						}
  					},
  					{
  						"name": "container2",
  						"securityContext": {
  							"runAsUser": 100
  						}
  					}
  				]
  			}
  		}
  	},
  	"parameters": {
  		"runAsUser": {
  			"rule": "RunAsAny"
  		}
  	}
  }
  count(results) == 0
}

test_two_containers_run_as_non_root_users_are_not_root {
  results := violation with input as
  {
  	"review": {
  	  "kind": {
  	    "kind": "Pod"
  	  },
  		"object": {
  			"spec": {
  				"containers": [{
  						"name": "container1",
  						"securityContext": {
  							"runAsUser": 1
  						}
  					},
  					{
  						"name": "container2",
  						"securityContext": {
  							"runAsUser": 100
  						}
  					}
  				]
  			}
  		}
  	},
  	"parameters": {
  		"runAsUser": {
  			"rule": "MustRunAsNonRoot"
  		}
  	}
  }
  count(results) == 0
}

test_two_containers_run_as_non_root_one_is_root {
  results := violation with input as
  {
  	"review": {
  	  "kind": {
  	    "kind": "Pod"
  	  },
  		"object": {
  			"spec": {
  				"containers": [{
  						"name": "container1",
  						"securityContext": {
  							"runAsUser": 1
  						}
  					},
  					{
  						"name": "container2",
  						"securityContext": {
  							"runAsUser": 0
  						}
  					}
  				]
  			}
  		}
  	},
  	"parameters": {
  		"runAsUser": {
  			"rule": "MustRunAsNonRoot"
  		}
  	}
  }
  count(results) > 0
}

test_two_containers_run_in_range_both_in_range {
  results := violation with input as
  {
  	"review": {
  	  "kind": {
  	    "kind": "Pod"
  	  },
  		"object": {
  			"spec": {
  				"containers": [{
  						"name": "container1",
  						"securityContext": {
  							"runAsUser": 150
  						}
  					},
  					{
  						"name": "container2",
  						"securityContext": {
  							"runAsUser": 103
  						}
  					}
  				]
  			}
  		}
  	},
  	"constraint": {
  		"spec": {
  			"parameters": {
  				"runAsUser": {
  					"rule": "MustRunAs",
  					"ranges": [{
  						"min": 100,
  						"max": 200
  					}]
  				}
  			}
  		}
  	}
  }
  count(results) == 0
}

test_two_containers_run_in_range_one_in_range {
  results := violation with input as
  {
  	"review": {
  	  "kind": {
  	    "kind": "Pod"
  	  },
  		"object": {
  			"spec": {
  				"containers": [{
  						"name": "container1",
  						"securityContext": {
  							"runAsUser": 150
  						}
  					},
  					{
  						"name": "container2",
  						"securityContext": {
  							"runAsUser": 13
  						}
  					}
  				]
  			}
  		}
  	},
  	"parameters": {
  		"runAsUser": {
  			"rule": "MustRunAs",
  			"ranges": [{
  				"min": 100,
  				"max": 200
  			}]
  		}
  	}
  }
  count(results) > 0
}

test_two_containers_run_in_range_neither_in_range {
  results := violation with input as
  {
  	"review": {
  	  "kind": {
  	    "kind": "Pod"
  	  },
  		"object": {
  			"spec": {
  				"containers": [{
  						"name": "container1",
  						"securityContext": {
  							"runAsUser": 150
  						}
  					},
  					{
  						"name": "container2",
  						"securityContext": {
  							"runAsUser": 130
  						}
  					}
  				]
  			}
  		}
  	},
  	"parameters": {
  		"runAsUser": {
  			"rule": "MustRunAs",
  			"ranges": [{
  				"min": 100,
  				"max": 100
  			}, {
  				"min": 250,
  				"max": 250
  			}]
  		}
  	}
  }
  count(results) > 0
}

test_one_container_one_initcontainer_run_in_range_user_in_range {
  results := violation with input as
  {
  	"review": {
  	  "kind": {
  	    "kind": "Pod"
  	  },
  		"object": {
  			"spec": {
  				"containers": [{
  					"name": "container1",
  					"securityContext": {
  						"runAsUser": 150
  					}
  				}],
  				"initContainers": [{
  					"name": "initContainer1",
  					"securityContext": {
  						"runAsUser": 150
  					}
  				}]
  			}
  		}
  	},
  	"parameters": {
  		"runAsUser": {
  			"rule": "MustRunAs",
  			"ranges": [{
  				"min": 100,
  				"max": 200
  			}]
  		}
  	}
  }
  count(results) == 0
}

test_one_container_one_initcontainer_run_in_range_user_not_in_range {
  results := violation with input as
  {
  	"review": {
  	  "kind": {
  	    "kind": "Pod"
  	  },
  		"object": {
  			"spec": {
  				"containers": [{
  					"name": "container1",
  					"securityContext": {
  						"runAsUser": 150
  					}
  				}],
  				"initContainers": [{
  					"name": "initContainer1",
  					"securityContext": {
  						"runAsUser": 250
  					}
  				}]
  			}
  		}
  	},
  	"parameters": {
  		"runAsUser": {
  			"rule": "MustRunAs",
  			"ranges": [{
  				"min": 100,
  				"max": 200
  			}]
  		}
  	}
  }
  count(results) > 0
}

test_one_container_empty_security_context {
  results := violation with input as
  {
  	"review": {
  	  "kind": {
  	    "kind": "Pod"
  	  },
  		"object": {
  			"spec": {
  				"containers": [{
  					"name": "container1",
  					"securityContext": {}
  				}]
  			}
  		}
  	},
  	"parameters": {
  		"runAsUser": {
  			"rule": "MustRunAs",
  			"ranges": [{
  				"min": 100,
  				"max": 200
  			}]
  		}
  	}
  }
  count(results) > 0
}

test_one_container_no_security_context {
  results := violation with input as
  {
  	"review": {
  	  "kind": {
  	    "kind": "Pod"
  	  },
  		"object": {
  			"spec": {
  				"containers": [{
  					"name": "container1",
  				}]
  			}
  		}
  	},
  	"parameters": {
  		"runAsUser": {
  			"rule": "MustRunAs",
  			"ranges": [{
  				"min": 100,
  				"max": 200
  			}]
  		}
  	}
  }
  count(results) > 0
}

test_one_container_no_security_context_RunAsAny {
  results := violation with input as
  {
  	"review": {
  	  "kind": {
  	    "kind": "Pod"
  	  },
  		"object": {
  			"spec": {
  				"containers": [{
  					"name": "container1",
  				}]
  			}
  		}
  	},
  	"parameters": {
  		"runAsUser": {
  			"rule": "RunAsAny",
  			"ranges": [{
  				"min": 100,
  				"max": 200
  			}]
  		}
  	}
  }
  count(results) == 0
}

test_one_container_empty_security_context_empty_pod_security_context {
  results := violation with input as
  {
  	"review": {
      "kind": {
        "kind": "Pod"
      },
  		"object": {
  			"spec": {
    			"securityContext": {},
  				"containers": [{
  					"name": "container1",
  					"securityContext": {}
  				}]
  			}
  		}
  	},
  	"parameters": {
  		"runAsUser": {
  			"rule": "MustRunAs",
  			"ranges": [{
  				"min": 100,
  				"max": 200
  			}]
  		}
  	}
  }
  count(results) > 0
}

test_one_container_no_security_context_no_pod_security_context {
  results := violation with input as
  {
  	"review": {
      "kind": {
        "kind": "Pod"
      },
  		"object": {
  			"spec": {
  				"containers": [{
  					"name": "container1",
  				}]
  			}
  		}
  	},
  	"parameters": {
  		"runAsUser": {
  			"rule": "MustRunAs",
  			"ranges": [{
  				"min": 100,
  				"max": 200
  			}]
  		}
  	}
  }
  count(results) > 0
}

test_one_container_pod_defined_run_in_range_user_in_range {
  results := violation with input as
  {
  	"review": {
      "kind": {
        "kind": "Pod"
      },
  		"object": {
  			"spec": {
  			  "securityContext": {
  			    "runAsUser": 150
  			  },
  				"containers": [{
  					"name": "container1",
  					"securityContext": {}
  				}]
  			}
  		}
  	},
  	"parameters": {
  		"runAsUser": {
  			"rule": "MustRunAs",
  			"ranges": [{
  				"min": 100,
  				"max": 200
  			}]
  		}
  	}
  }
  count(results) == 0
}

test_one_container_pod_defined_run_in_range_user_not_in_range {
  results := violation with input as
  {
  	"review": {
  	  "kind": {
  	    "kind": "Pod"
  	  },
  		"object": {
  			"spec": {
  			  "securityContext": {
  			    "runAsUser": 250
  			  },
  				"containers": [{
  					"name": "container1",
  					"securityContext": {}
  				}]
  			}
  		}
  	},
  	"parameters": {
  		"runAsUser": {
  			"rule": "MustRunAs",
  			"ranges": [{
  				"min": 100,
  				"max": 200
  			}]
  		}
  	}
  }
  count(results) > 0
}

test_one_container_pod_defined_run_in_range_container_overrides_user_in_range {
  results := violation with input as
  {
  	"review": {
  		"object": {
  		  "kind": "Pod",
  			"spec": {
  			  "securityContext": {
  			    "runAsUser": 250
  			  },
  				"containers": [{
  					"name": "container1",
  					"securityContext": {
  					  "runAsUser": 150
  					}
  				}]
  			}
  		}
  	},
  	"parameters": {
  		"runAsUser": {
  			"rule": "MustRunAs",
  			"ranges": [{
  				"min": 100,
  				"max": 200
  			}]
  		}
  	}
  }
  count(results) == 0
}

test_one_container_pod_defined_run_in_range_container_overrides_user_not_in_range {
  results := violation with input as
  {
  	"review": {
  		"object": {
        "kind": "Pod",
  			"spec": {
  			  "securityContext": {
  			    "runAsUser": 150
  			  },
  				"containers": [{
  					"name": "container1",
  					"securityContext": {
  					  "runAsUser": 250
  					}
  				}]
  			}
  		}
  	},
  	"parameters": {
  		"runAsUser": {
  			"rule": "MustRunAs",
  			"ranges": [{
  				"min": 100,
  				"max": 200
  			}]
  		}
  	}
  }
  count(results) > 0
}
